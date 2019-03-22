/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package comm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/hyperledger/fabric/common/flogging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var commLogger = flogging.MustGetLogger("comm")
var credSupport *CredentialSupport
var once sync.Once

// CredentialSupport type manages credentials used for gRPC client connections
type CredentialSupport struct {
	sync.RWMutex
	AppRootCAsByChain     map[string][][]byte
	OrdererRootCAsByChain map[string][][]byte
	ClientRootCAs         [][]byte
	ServerRootCAs         [][]byte
	clientCert            tls.Certificate
}

// GetCredentialSupport returns the singleton CredentialSupport instance
func GetCredentialSupport() *CredentialSupport {

	once.Do(func() {
		credSupport = &CredentialSupport{
			AppRootCAsByChain:     make(map[string][][]byte),
			OrdererRootCAsByChain: make(map[string][][]byte),
		}
	})
	return credSupport
}

// SetClientCertificate sets the tls.Certificate to use for gRPC client
// connections
func (cs *CredentialSupport) SetClientCertificate(cert tls.Certificate) {
	cs.clientCert = cert
}

// GetClientCertificate returns the client certificate of the CredentialSupport
func (cs *CredentialSupport) GetClientCertificate() tls.Certificate {
	return cs.clientCert
}

// GetDeliverServiceCredentials returns gRPC transport credentials for given
// channel to be used by gRPC clients which communicate with ordering service endpoints.
// If the channel isn't found, an error is returned.
func (cs *CredentialSupport) GetDeliverServiceCredentials(channelID string) (credentials.TransportCredentials, error) {
	cs.RLock()
	defer cs.RUnlock()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cs.clientCert},
	}
	certPool := x509.NewCertPool()

	rootCACerts, exists := cs.OrdererRootCAsByChain[channelID]
	if !exists {
		commLogger.Errorf("Attempted to obtain root CA certs of a non existent channel: %s", channelID)
		return nil, fmt.Errorf("didn't find any root CA certs for channel %s", channelID)
	}

	for _, cert := range rootCACerts {
		block, _ := pem.Decode(cert)
		if block != nil {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err == nil {
				certPool.AddCert(cert)
			} else {
				commLogger.Warningf("Failed to add root cert to credentials (%s)", err)
			}
		} else {
			commLogger.Warning("Failed to add root cert to credentials")
		}
	}
	tlsConfig.RootCAs = certPool
	return credentials.NewTLS(tlsConfig), nil
}

// GetPeerCredentials returns gRPC transport credentials for use by gRPC
// clients which communicate with remote peer endpoints.
func (cs *CredentialSupport) GetPeerCredentials() credentials.TransportCredentials {
	cs.RLock()
	defer cs.RUnlock()

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cs.clientCert},
	}
	certPool := x509.NewCertPool()
	appRootCAs := [][]byte{}
	for _, appRootCA := range cs.AppRootCAsByChain {
		appRootCAs = append(appRootCAs, appRootCA...)
	}
	// also need to append statically configured root certs
	appRootCAs = append(appRootCAs, cs.ServerRootCAs...)
	// loop through the app root CAs
	for _, appRootCA := range appRootCAs {
		err := AddPemToCertPool(appRootCA, certPool)
		if err != nil {
			commLogger.Warningf("Failed adding certificates to peer's client TLS trust pool: %s", err)
		}
	}

	tlsConfig.RootCAs = certPool
	return credentials.NewTLS(tlsConfig)
}

func getEnv(key, def string) string {
	val := os.Getenv(key)
	if len(val) > 0 {
		return val
	} else {
		return def
	}
}

// NewClientConnectionWithAddress Returns a new grpc.ClientConn to the given address
func NewClientConnectionWithAddress(
	peerAddress string,
	block bool,
	dialTimeout time.Duration,
	tslEnabled bool,
	creds credentials.TransportCredentials,
	ka *KeepaliveOptions,
) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	if ka != nil {
		opts = ClientKeepaliveOptions(ka)
	} else {
		// set to the default options
		opts = ClientKeepaliveOptions(DefaultKeepaliveOptions)
	}

	if tslEnabled {
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
	if block {
		opts = append(opts, grpc.WithBlock())
	}
	opts = append(opts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(MaxRecvMsgSize),
		grpc.MaxCallSendMsgSize(MaxSendMsgSize),
	))
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, peerAddress, opts...)
	if err != nil {
		return nil, err
	}
	return conn, err
}
