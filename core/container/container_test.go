/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package container_test

import (
	"bytes"
	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/hyperledger/fabric/core/chaincode/persistence"
	"github.com/hyperledger/fabric/core/common/ccprovider"
	"github.com/hyperledger/fabric/core/container"
	"github.com/hyperledger/fabric/core/container/ccintf"
	"github.com/hyperledger/fabric/core/container/mock"
	"github.com/pkg/errors"
)

var _ = Describe("Container", func() {
	Describe("Router", func() {
		var (
			fakeDockerVM        *mock.VM
			fakeExternalVM      *mock.VM
			fakePackageProvider *mock.PackageProvider
			fakeInstance        *mock.Instance
			router              *container.Router
		)

		BeforeEach(func() {
			fakeDockerVM = &mock.VM{}
			fakeExternalVM = &mock.VM{}
			fakeInstance = &mock.Instance{}
			fakePackageProvider = &mock.PackageProvider{}
			fakePackageProvider.GetChaincodePackageReturns(
				&persistence.ChaincodePackageMetadata{
					Type: "package-type",
					Path: "package-path",
				},
				ioutil.NopCloser(bytes.NewBuffer([]byte("code-bytes"))),
				nil,
			)

			router = &container.Router{
				DockerVM:        fakeDockerVM,
				ExternalVM:      fakeExternalVM,
				PackageProvider: fakePackageProvider,
			}
		})

		Describe("Build", func() {
			BeforeEach(func() {
				fakeExternalVM.BuildReturns(fakeInstance, nil)
			})

			It("passes through to the external impl", func() {
				err := router.Build(
					&ccprovider.ChaincodeContainerInfo{
						PackageID: "package-id",
						Type:      "type",
						Path:      "path",
						Name:      "name",
						Version:   "version",
					},
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeDockerVM.BuildCallCount()).To(Equal(0))
				Expect(fakeExternalVM.BuildCallCount()).To(Equal(1))
				ccci, codeStream := fakeExternalVM.BuildArgsForCall(0)
				Expect(ccci).To(Equal(&ccprovider.ChaincodeContainerInfo{
					PackageID: "package-id",
					Type:      "package-type",
					Path:      "package-path",
				}))
				codePackage, err := ioutil.ReadAll(codeStream)
				Expect(err).NotTo(HaveOccurred())
				Expect(codePackage).To(Equal([]byte("code-bytes")))
			})

			Context("when the package provider returns an error", func() {
				BeforeEach(func() {
					fakePackageProvider.GetChaincodePackageReturns(nil, nil, errors.New("fake-package-error"))
				})

				It("wraps and returns the error", func() {
					err := router.Build(
						&ccprovider.ChaincodeContainerInfo{
							PackageID: "package-id",
							Type:      "package-type",
							Path:      "package-path",
						},
					)

					Expect(err).To(MatchError("get chaincode package for external build failed: fake-package-error"))
				})
			})

			Context("when the external impl returns an error", func() {
				BeforeEach(func() {
					fakeExternalVM.BuildReturns(nil, errors.New("fake-external-error"))
					fakeDockerVM.BuildReturns(fakeInstance, errors.New("fake-docker-error"))
				})

				It("falls back to the docker impl", func() {
					err := router.Build(
						&ccprovider.ChaincodeContainerInfo{
							PackageID: "package-id",
							Type:      "package-type",
							Path:      "package-path",
						},
					)
					Expect(err).To(MatchError("failed external (fake-external-error) and docker build: fake-docker-error"))
					Expect(fakeExternalVM.BuildCallCount()).To(Equal(1))
					Expect(fakeDockerVM.BuildCallCount()).To(Equal(1))
					ccci, codeStream := fakeDockerVM.BuildArgsForCall(0)
					Expect(ccci).To(Equal(&ccprovider.ChaincodeContainerInfo{
						PackageID: "package-id",
						Type:      "package-type",
						Path:      "package-path",
					}))
					codePackage, err := ioutil.ReadAll(codeStream)
					Expect(err).NotTo(HaveOccurred())
					Expect(codePackage).To(Equal([]byte("code-bytes")))
				})

				Context("when the package provider returns an error", func() {
					BeforeEach(func() {
						fakePackageProvider.GetChaincodePackageReturnsOnCall(1, nil, nil, errors.New("fake-package-error"))
					})

					It("wraps and returns the error", func() {
						err := router.Build(
							&ccprovider.ChaincodeContainerInfo{
								PackageID: "package-id",
								Type:      "package-type",
								Path:      "package-path",
							},
						)

						Expect(err).To(MatchError("get chaincode package for docker build failed: fake-package-error"))
					})
				})

			})
		})

		Describe("Post-build operations", func() {
			BeforeEach(func() {
				fakeExternalVM.BuildReturns(fakeInstance, nil)
				err := router.Build(&ccprovider.ChaincodeContainerInfo{
					PackageID: "fake-id",
					Type:      "type",
					Path:      "path",
					Name:      "name",
					Version:   "version",
				},
				)
				Expect(err).NotTo(HaveOccurred())
			})

			Describe("Start", func() {
				BeforeEach(func() {
					fakeInstance.StartReturns(errors.New("fake-start-error"))
				})

				It("passes through to the docker impl", func() {
					err := router.Start(
						ccintf.CCID("fake-id"),
						&ccintf.PeerConnection{
							Address: "peer-address",
							TLSConfig: &ccintf.TLSConfig{
								ClientKey:  []byte("key"),
								ClientCert: []byte("cert"),
								RootCert:   []byte("root"),
							},
						},
					)

					Expect(err).To(MatchError("fake-start-error"))
					Expect(fakeInstance.StartCallCount()).To(Equal(1))
					Expect(fakeInstance.StartArgsForCall(0)).To(Equal(&ccintf.PeerConnection{
						Address: "peer-address",
						TLSConfig: &ccintf.TLSConfig{
							ClientKey:  []byte("key"),
							ClientCert: []byte("cert"),
							RootCert:   []byte("root"),
						},
					}))
				})

				Context("when the chaincode has not yet been built", func() {
					It("returns an error", func() {
						err := router.Start(
							ccintf.CCID("missing-name"),
							&ccintf.PeerConnection{
								Address: "peer-address",
							},
						)
						Expect(err).To(MatchError("instance has not yet been built, cannot be started"))
					})
				})
			})

			Describe("Stop", func() {
				BeforeEach(func() {
					fakeInstance.StopReturns(errors.New("Boo"))
				})

				It("passes through to the docker impl", func() {
					err := router.Stop(ccintf.CCID("fake-id"))
					Expect(err).To(MatchError("Boo"))
					Expect(fakeInstance.StopCallCount()).To(Equal(1))
				})

				Context("when the chaincode has not yet been built", func() {
					It("returns an error", func() {
						err := router.Stop(ccintf.CCID("missing-name"))
						Expect(err).To(MatchError("instance has not yet been built, cannot be stopped"))
					})
				})
			})

			Describe("Wait", func() {
				BeforeEach(func() {
					fakeInstance.WaitReturns(7, errors.New("fake-wait-error"))
				})

				It("passes through to the docker impl", func() {
					res, err := router.Wait(
						ccintf.CCID("fake-id"),
					)
					Expect(res).To(Equal(7))
					Expect(err).To(MatchError("fake-wait-error"))
					Expect(fakeInstance.WaitCallCount()).To(Equal(1))
				})

				Context("when the chaincode has not yet been built", func() {
					It("returns an error", func() {
						_, err := router.Wait(ccintf.CCID("missing-name"))
						Expect(err).To(MatchError("instance has not yet been built, cannot wait"))
					})
				})
			})
		})
	})
})
