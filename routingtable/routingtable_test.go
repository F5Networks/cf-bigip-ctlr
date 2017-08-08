package routingtable_test

import (
	"github.com/F5Networks/cf-bigip-ctlr/config"
	"github.com/F5Networks/cf-bigip-ctlr/routingtable"
	"github.com/F5Networks/cf-bigip-ctlr/test_util"

	routing_api_models "code.cloudfoundry.org/routing-api/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = Describe("RoutingTable", func() {
	var (
		routingTable    *routingtable.RoutingTable
		modificationTag routing_api_models.ModificationTag
		logger          = test_util.NewTestZapLogger("routing-table-test")
		c               *config.Config
	)

	BeforeEach(func() {
		c = config.DefaultConfig()
		c.PruneStaleDropletsInterval = 1
		routingTable = routingtable.NewRoutingTable(logger, c)
		modificationTag = routing_api_models.ModificationTag{Guid: "abc", Index: 1}
	})

	Describe("UpsertBackendServerKey", func() {
		var (
			routingKey routingtable.RoutingKey
		)

		BeforeEach(func() {
			routingKey = routingtable.RoutingKey{Port: 12}
			routingTable = routingtable.NewRoutingTable(logger, c)
			modificationTag = routing_api_models.ModificationTag{Guid: "abc", Index: 5}
		})

		Context("when the routing key does not exist", func() {
			var (
				backendServerInfo routingtable.BackendServerInfo
			)

			BeforeEach(func() {
				backendServerInfo = createBackendServerInfo("some-ip", 1234, modificationTag)
			})

			It("inserts the routing key with its backends", func() {
				updated := routingTable.UpsertBackendServerKey(routingKey, backendServerInfo)
				Expect(updated).To(BeTrue())
				Expect(routingTable.NumberOfRoutes()).To(Equal(1))
				Expect(routingTable.RouteExists(routingKey)).To(BeTrue())
			})
		})

		Context("when the routing key does exist", func() {
			var backendServerInfo routingtable.BackendServerInfo

			BeforeEach(func() {
				backendServerInfo = createBackendServerInfo("some-ip", 1234, modificationTag)
				updated := routingTable.UpsertBackendServerKey(routingKey, backendServerInfo)
				Expect(updated).To(BeTrue())
				Expect(routingTable.RouteExists(routingKey)).To(BeTrue())
				Expect(routingTable.NumberOfRoutes()).To(Equal(1))
			})

			Context("when current entry is succeeded by new entry", func() {
				BeforeEach(func() {
					modificationTag.Increment()
				})

				It("updates the routing entry", func() {
					sameBackendServerInfo := createBackendServerInfo("some-ip", 1234, modificationTag)
					updated := routingTable.UpsertBackendServerKey(routingKey, sameBackendServerInfo)
					Expect(updated).To(BeFalse())
					Expect(logger).To(gbytes.Say("applying-change-to-table"))
				})

				It("does not update routing configuration", func() {
					sameBackendServerInfo := createBackendServerInfo("some-ip", 1234, modificationTag)
					updated := routingTable.UpsertBackendServerKey(routingKey, sameBackendServerInfo)
					Expect(updated).To(BeFalse())
				})
			})

			Context("and a new backend is provided", func() {
				It("updates the routing entry's backends", func() {
					anotherModificationTag := routing_api_models.ModificationTag{Guid: "def", Index: 0}
					differentBackendServerInfo := createBackendServerInfo("some-other-ip", 1234, anotherModificationTag)
					updated := routingTable.UpsertBackendServerKey(routingKey, differentBackendServerInfo)
					Expect(updated).To(BeTrue())
					Expect(routingTable.NumberOfRoutes()).To(Equal(1))
					Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(2))
					Expect(routingTable.BackendExists(
						routingKey,
						routingtable.BackendServerKey{Address: "some-other-ip", Port: 1234},
					)).To(BeTrue())

				})
			})

			Context("when current entry is fresher than incoming entry", func() {

				BeforeEach(func() {
					modificationTag.Index--
				})

				It("should not update routing table", func() {
					newBackendServerInfo := createBackendServerInfo("some-ip", 1234, modificationTag)
					updated := routingTable.UpsertBackendServerKey(routingKey, newBackendServerInfo)
					Expect(updated).To(BeFalse())
					Expect(logger).To(gbytes.Say("skipping-stale-event"))
				})
			})
		})
	})

	Describe("DeleteBackendServerKey", func() {
		var (
			routingKey         routingtable.RoutingKey
			badRoutingKey      routingtable.RoutingKey
			backendServerKey   routingtable.BackendServerKey
			backendServerInfo1 routingtable.BackendServerInfo
			backendServerInfo2 routingtable.BackendServerInfo
		)
		BeforeEach(func() {
			routingKey = routingtable.RoutingKey{Port: 12}
			badRoutingKey = routingtable.RoutingKey{Port: 13}
			backendServerKey = routingtable.BackendServerKey{Address: "some-ip", Port: 1234}
			backendServerInfo1 = createBackendServerInfo("some-ip", 1234, modificationTag)
			updated := routingTable.UpsertBackendServerKey(routingKey, backendServerInfo1)
			Expect(updated).To(BeTrue())
			Expect(routingTable.NumberOfRoutes()).To(Equal(1))
			Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(1))
		})

		Context("when the routing key does not exist", func() {
			It("it does not causes any changes or errors", func() {
				updated := routingTable.DeleteBackendServerKey(badRoutingKey, backendServerInfo1)
				Expect(updated).To(BeFalse())
				Expect(routingTable.NumberOfRoutes()).To(Equal(1))
				Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(1))
			})
		})

		Context("when the routing key does exist", func() {
			BeforeEach(func() {
				backendServerInfo2 = createBackendServerInfo("some-other-ip", 1235, modificationTag)
				updated := routingTable.UpsertBackendServerKey(routingKey, backendServerInfo2)
				Expect(updated).To(BeTrue())
				Expect(routingTable.NumberOfRoutes()).To(Equal(1))
				Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(2))
			})

			Context("and the backend does not exist ", func() {
				It("does not causes any changes or errors", func() {
					backendServerInfo1 = createBackendServerInfo("some-missing-ip", 1236, modificationTag)
					ok := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
					Expect(ok).To(BeFalse())
					Expect(routingTable.NumberOfRoutes()).To(Equal(1))
					Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(2))
				})
			})

			Context("and the backend does exist", func() {
				It("deletes the backend", func() {
					updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
					Expect(updated).To(BeTrue())
					Expect(logger).To(gbytes.Say("removing-from-table"))
					Expect(routingTable.NumberOfRoutes()).To(Equal(1))
					Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(1))
					Expect(routingTable.BackendExists(routingKey, backendServerKey)).To(BeFalse())
				})

				Context("when a modification tag has the same guid but current index is greater", func() {
					BeforeEach(func() {
						backendServerInfo1.ModificationTag.Index--
					})

					It("does not deletes the backend", func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
						Expect(updated).To(BeFalse())
						Expect(logger).To(gbytes.Say("skipping-stale-event"))
						Expect(routingTable.BackendExists(routingKey, backendServerKey)).To(BeTrue())
					})
				})

				Context("when a modification tag has different guid", func() {

					BeforeEach(func() {
						backendServerInfo1.ModificationTag = routing_api_models.ModificationTag{Guid: "def"}
					})

					It("deletes the backend", func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
						Expect(updated).To(BeTrue())
						Expect(logger).To(gbytes.Say("removing-from-table"))
						Expect(routingTable.BackendExists(routingKey, backendServerKey)).To(BeFalse())
					})
				})

				Context("when there are no more backends left", func() {
					BeforeEach(func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
						Expect(updated).To(BeTrue())
						Expect(routingTable.NumberOfBackends(routingKey)).To(Equal(1))
					})

					It("deletes the entry", func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo2)
						Expect(updated).To(BeTrue())
						Expect(routingTable.NumberOfRoutes()).Should(Equal(0))
					})
				})
			})
		})
	})

	Describe("PruningCycle", func() {
		var (
			routingKey1 routingtable.RoutingKey
		)
		BeforeEach(func() {
			routingKey1 = routingtable.RoutingKey{Port: 12}
			backendServerInfo1 := createBackendServerInfo("some-ip-1", 1234, modificationTag)
			updated := routingTable.UpsertBackendServerKey(routingKey1, backendServerInfo1)
			Expect(updated).To(BeTrue())

			Expect(routingTable.NumberOfRoutes()).To(Equal(1))
			Expect(routingTable.NumberOfBackends(routingKey1)).To(Equal(1))
		})

		JustBeforeEach(func() {
			routingTable.StartPruningCycle()
		})

		AfterEach(func() {
			routingTable.StopPruningCycle()
		})

		Context("when it has expired entries", func() {
			BeforeEach(func() {
				backendServerInfo2 := routingtable.BackendServerInfo{
					Address:         "some-ip-2",
					Port:            1235,
					ModificationTag: modificationTag,
					TTL:             20,
				}
				updated := routingTable.UpsertBackendServerKey(routingKey1, backendServerInfo2)
				Expect(updated).To(BeTrue())
				Expect(routingTable.NumberOfRoutes()).To(Equal(1))
				Expect(routingTable.NumberOfBackends(routingKey1)).To(Equal(2))
			})

			It("prunes only the expired entries", func() {
				Eventually(func() int {
					return routingTable.NumberOfBackends(routingKey1)
				}, 5).Should(Equal(1))
				Consistently(func() int {
					return routingTable.NumberOfBackends(routingKey1)
				}, 2).Should(Equal(1))
			})
		})

		Context("when all the backends expire for given routing key", func() {
			It("prunes the expired entries and deletes the routing key", func() {
				Eventually(routingTable.NumberOfRoutes, 5).Should(Equal(0))
			})
		})
	})
})

func createBackendServerInfo(
	address string,
	port uint16,
	tag routing_api_models.ModificationTag,
) routingtable.BackendServerInfo {
	return routingtable.BackendServerInfo{Address: address, Port: port, ModificationTag: tag}
}
