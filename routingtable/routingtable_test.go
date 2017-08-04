package routingtable_test

import (
	"reflect"
	"time"

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
		backendServerKey routingtable.BackendServerKey
		routingTable     *routingtable.RoutingTable
		modificationTag  routing_api_models.ModificationTag
		logger           = test_util.NewTestZapLogger("routing-table-test")
		c                *config.Config
	)

	BeforeEach(func() {
		c = config.DefaultConfig()
		routingTable = routingtable.NewRoutingTable(logger, c)
		modificationTag = routing_api_models.ModificationTag{Guid: "abc", Index: 1}
	})

	Describe("Set", func() {
		var (
			routingKey           routingtable.RoutingKey
			routingTableEntry    routingtable.Entry
			backendServerDetails *routingtable.BackendServerDetails
			now                  time.Time
		)

		BeforeEach(func() {
			routingKey = routingtable.RoutingKey{Port: 12}
			backendServerKey = routingtable.BackendServerKey{Address: "some-ip-1", Port: 1234}
			now = time.Now()
			backendServerDetails = &routingtable.BackendServerDetails{ModificationTag: modificationTag, TTL: 120, UpdatedTime: now}
			backends := map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
				backendServerKey: backendServerDetails,
			}
			routingTableEntry = routingtable.Entry{Backends: backends}
		})

		Context("when a new entry is added", func() {
			It("adds the entry", func() {
				ok := compareEntries(routingTable, routingKey, routingTableEntry)
				Expect(ok).To(BeTrue())
				Expect(routingTable.Get(routingKey)).To(Equal(routingTableEntry))
				Expect(routingTable.Size()).To(Equal(1))
			})
		})

		Context("when setting pre-existing routing key", func() {
			var (
				existingRoutingTableEntry routingtable.Entry
				newBackendServerKey       routingtable.BackendServerKey
			)

			BeforeEach(func() {
				newBackendServerKey = routingtable.BackendServerKey{
					Address: "some-ip-2",
					Port:    1234,
				}
				existingRoutingTableEntry = routingtable.Entry{
					Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
						backendServerKey:    backendServerDetails,
						newBackendServerKey: &routingtable.BackendServerDetails{ModificationTag: modificationTag, TTL: 120, UpdatedTime: now},
					},
				}
				ok := compareEntries(routingTable, routingKey, existingRoutingTableEntry)
				Expect(ok).To(BeTrue())
				Expect(routingTable.Size()).To(Equal(1))
			})

			Context("with different value", func() {
				verifyChangedValue := func(routingTableEntry routingtable.Entry) {
					ok := compareEntries(routingTable, routingKey, routingTableEntry)
					Expect(ok).To(BeTrue())
					Expect(routingTable.Get(routingKey)).Should(Equal(routingTableEntry))
				}

				Context("when number of backends are different", func() {
					It("overwrites the value", func() {
						routingTableEntry := routingtable.Entry{
							Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
								routingtable.BackendServerKey{
									Address: "some-ip-1",
									Port:    1234,
								}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: now},
							},
						}
						verifyChangedValue(routingTableEntry)
					})
				})

				Context("when at least one backend server info is different", func() {
					It("overwrites the value", func() {
						routingTableEntry := routingtable.Entry{
							Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
								routingtable.BackendServerKey{Address: "some-ip-1", Port: 1234}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: now},
								routingtable.BackendServerKey{Address: "some-ip-2", Port: 2345}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: now},
							},
						}
						verifyChangedValue(routingTableEntry)
					})
				})

				Context("when all backend servers info are different", func() {
					It("overwrites the value", func() {
						routingTableEntry := routingtable.Entry{
							Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
								routingtable.BackendServerKey{Address: "some-ip-1", Port: 3456}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: now},
								routingtable.BackendServerKey{Address: "some-ip-2", Port: 2345}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: now},
							},
						}
						verifyChangedValue(routingTableEntry)
					})
				})

				Context("when modificationTag is different", func() {
					It("overwrites the value", func() {
						routingTableEntry := routingtable.Entry{
							Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
								routingtable.BackendServerKey{Address: "some-ip-1", Port: 1234}: &routingtable.BackendServerDetails{ModificationTag: routing_api_models.ModificationTag{Guid: "different-guid"}, UpdatedTime: now},
								routingtable.BackendServerKey{Address: "some-ip-2", Port: 1234}: &routingtable.BackendServerDetails{ModificationTag: routing_api_models.ModificationTag{Guid: "different-guid"}, UpdatedTime: now},
							},
						}
						verifyChangedValue(routingTableEntry)
					})
				})

				Context("when TTL is different", func() {
					It("overwrites the value", func() {
						routingTableEntry := routingtable.Entry{
							Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
								routingtable.BackendServerKey{Address: "some-ip-1", Port: 1234}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, TTL: 110, UpdatedTime: now},
								routingtable.BackendServerKey{Address: "some-ip-2", Port: 1234}: &routingtable.BackendServerDetails{ModificationTag: modificationTag, TTL: 110, UpdatedTime: now},
							},
						}
						verifyChangedValue(routingTableEntry)
					})
				})
			})

			Context("with same value", func() {
				It("returns false", func() {
					routingTableEntry := routingtable.Entry{
						Backends: map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
							backendServerKey:    &routingtable.BackendServerDetails{ModificationTag: modificationTag, TTL: 120, UpdatedTime: now},
							newBackendServerKey: &routingtable.BackendServerDetails{ModificationTag: modificationTag, TTL: 120, UpdatedTime: now},
						},
					}
					ok := compareEntries(routingTable, routingKey, routingTableEntry)
					Expect(ok).To(BeFalse())
					test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), existingRoutingTableEntry)
				})
			})
		})
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
				routingTableEntry routingtable.Entry
				backendServerInfo routingtable.BackendServerInfo
			)

			BeforeEach(func() {
				backendServerInfo = createBackendServerInfo("some-ip", 1234, modificationTag)
				routingTableEntry = routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo})
			})

			It("inserts the routing key with its backends", func() {
				updated := routingTable.UpsertBackendServerKey(routingKey, backendServerInfo)
				Expect(updated).To(BeTrue())
				Expect(routingTable.Size()).To(Equal(1))
				test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), routingTableEntry)
			})
		})

		Context("when the routing key does exist", func() {
			var backendServerInfo routingtable.BackendServerInfo

			BeforeEach(func() {
				backendServerInfo = createBackendServerInfo("some-ip", 1234, modificationTag)
				existingRoutingTableEntry := routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo})
				updated := compareEntries(routingTable, routingKey, existingRoutingTableEntry)
				Expect(updated).To(BeTrue())
			})

			Context("when current entry is succeeded by new entry", func() {
				BeforeEach(func() {
					modificationTag.Increment()
				})

				It("updates the routing entry", func() {
					sameBackendServerInfo := createBackendServerInfo("some-ip", 1234, modificationTag)
					expectedRoutingTableEntry := routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{sameBackendServerInfo})
					routingTable.UpsertBackendServerKey(routingKey, sameBackendServerInfo)
					test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), expectedRoutingTableEntry)
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
					expectedRoutingTableEntry := routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo, differentBackendServerInfo})
					updated := routingTable.UpsertBackendServerKey(routingKey, differentBackendServerInfo)
					Expect(updated).To(BeTrue())
					test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), expectedRoutingTableEntry)
					actualDetails := routingTable.Get(routingKey).Backends[routingtable.BackendServerKey{Address: "some-other-ip", Port: 1234}]
					expectedDetails := expectedRoutingTableEntry.Backends[routingtable.BackendServerKey{Address: "some-other-ip", Port: 1234}]
					Expect(actualDetails.UpdatedTime.After(expectedDetails.UpdatedTime)).To(BeTrue())
				})
			})

			Context("when current entry is fresher than incoming entry", func() {

				var existingRoutingTableEntry routingtable.Entry

				BeforeEach(func() {
					existingRoutingTableEntry = routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{createBackendServerInfo("some-ip", 1234, modificationTag)})
					modificationTag.Index--
				})

				It("should not update routing table", func() {
					newBackendServerInfo := createBackendServerInfo("some-ip", 1234, modificationTag)
					updated := routingTable.UpsertBackendServerKey(routingKey, newBackendServerInfo)
					Expect(updated).To(BeFalse())
					Expect(logger).To(gbytes.Say("skipping-stale-event"))
					test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), existingRoutingTableEntry)
				})
			})
		})
	})

	Describe("DeleteBackendServerKey", func() {
		var (
			routingKey                routingtable.RoutingKey
			existingRoutingTableEntry routingtable.Entry
			backendServerInfo1        routingtable.BackendServerInfo
			backendServerInfo2        routingtable.BackendServerInfo
		)
		BeforeEach(func() {
			routingKey = routingtable.RoutingKey{Port: 12}
			backendServerInfo1 = createBackendServerInfo("some-ip", 1234, modificationTag)
		})

		Context("when the routing key does not exist", func() {
			It("it does not causes any changes or errors", func() {
				updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
				Expect(updated).To(BeFalse())
			})
		})

		Context("when the routing key does exist", func() {
			BeforeEach(func() {
				backendServerInfo2 = createBackendServerInfo("some-other-ip", 1235, modificationTag)
				existingRoutingTableEntry = routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo1, backendServerInfo2})
				updated := compareEntries(routingTable, routingKey, existingRoutingTableEntry)
				Expect(updated).To(BeTrue())
			})

			Context("and the backend does not exist ", func() {
				It("does not causes any changes or errors", func() {
					backendServerInfo1 = createBackendServerInfo("some-missing-ip", 1236, modificationTag)
					ok := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
					Expect(ok).To(BeFalse())
					Expect(routingTable.Get(routingKey)).Should(Equal(existingRoutingTableEntry))
				})
			})

			Context("and the backend does exist", func() {
				It("deletes the backend", func() {
					updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
					Expect(updated).To(BeTrue())
					Expect(logger).To(gbytes.Say("removing-from-table"))
					expectedRoutingTableEntry := routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo2})
					test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), expectedRoutingTableEntry)
				})

				Context("when a modification tag has the same guid but current index is greater", func() {
					BeforeEach(func() {
						backendServerInfo1.ModificationTag.Index--
					})

					It("does not deletes the backend", func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
						Expect(updated).To(BeFalse())
						Expect(logger).To(gbytes.Say("skipping-stale-event"))
						Expect(routingTable.Get(routingKey)).Should(Equal(existingRoutingTableEntry))
					})
				})

				Context("when a modification tag has different guid", func() {
					var expectedRoutingTableEntry routingtable.Entry

					BeforeEach(func() {
						expectedRoutingTableEntry = routingtable.NewRoutingTableEntry([]routingtable.BackendServerInfo{backendServerInfo2})
						backendServerInfo1.ModificationTag = routing_api_models.ModificationTag{Guid: "def"}
					})

					It("deletes the backend", func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
						Expect(updated).To(BeTrue())
						Expect(logger).To(gbytes.Say("removing-from-table"))
						test_util.RoutingTableEntryMatches(routingTable.Get(routingKey), expectedRoutingTableEntry)
					})
				})

				Context("when there are no more backends left", func() {
					BeforeEach(func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo1)
						Expect(updated).To(BeTrue())
					})

					It("deletes the entry", func() {
						updated := routingTable.DeleteBackendServerKey(routingKey, backendServerInfo2)
						Expect(updated).To(BeTrue())
						Expect(routingTable.Size()).Should(Equal(0))
					})
				})
			})
		})
	})

	Describe("PruneEntries", func() {
		var (
			defaultTTL  time.Duration
			routingKey1 routingtable.RoutingKey
			routingKey2 routingtable.RoutingKey
		)
		BeforeEach(func() {
			routingKey1 = routingtable.RoutingKey{Port: 12}
			backendServerKey := routingtable.BackendServerKey{Address: "some-ip-1", Port: 1234}
			backendServerDetails := &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: time.Now().Add(-10 * time.Second)}
			backendServerKey2 := routingtable.BackendServerKey{Address: "some-ip-2", Port: 1235}
			backendServerDetails2 := &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: time.Now().Add(-3 * time.Second)}
			backends := map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
				backendServerKey:  backendServerDetails,
				backendServerKey2: backendServerDetails2,
			}
			routingTableEntry := routingtable.Entry{Backends: backends}
			updated := compareEntries(routingTable, routingKey1, routingTableEntry)
			Expect(updated).To(BeTrue())

			routingKey2 = routingtable.RoutingKey{Port: 13}
			backendServerKey = routingtable.BackendServerKey{Address: "some-ip-3", Port: 1234}
			backendServerDetails = &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: time.Now().Add(-10 * time.Second)}
			backendServerKey2 = routingtable.BackendServerKey{Address: "some-ip-4", Port: 1235}
			backendServerDetails2 = &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: time.Now()}
			backends = map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
				backendServerKey:  backendServerDetails,
				backendServerKey2: backendServerDetails2,
			}
			routingTableEntry = routingtable.Entry{Backends: backends}
			updated = compareEntries(routingTable, routingKey2, routingTableEntry)
			Expect(updated).To(BeTrue())
		})

		JustBeforeEach(func() {
			routingTable.PruneEntries(defaultTTL)
		})

		Context("when it has expired entries", func() {
			BeforeEach(func() {
				defaultTTL = 5
			})

			It("prunes the expired entries", func() {
				Expect(routingTable.Entries).To(HaveLen(2))
				Expect(routingTable.Get(routingKey1).Backends).To(HaveLen(1))
				Expect(routingTable.Get(routingKey2).Backends).To(HaveLen(1))
			})

			Context("when all the backends expire for given routing key", func() {
				BeforeEach(func() {
					defaultTTL = 2
				})

				It("prunes the expired entries and deletes the routing key", func() {
					Expect(routingTable.Entries).To(HaveLen(1))
					Expect(routingTable.Get(routingKey2).Backends).To(HaveLen(1))
				})
			})
		})

		Context("when it has no expired entries", func() {
			BeforeEach(func() {
				defaultTTL = 20
			})

			It("does not prune entries", func() {
				Expect(routingTable.Entries).To(HaveLen(2))
				Expect(routingTable.Get(routingKey1).Backends).To(HaveLen(2))
				Expect(routingTable.Get(routingKey2).Backends).To(HaveLen(2))
			})
		})
	})

	Describe("BackendServerDetails", func() {
		var (
			now        = time.Now()
			defaultTTL = time.Duration(20)
		)

		Context("when backend details have TTL", func() {
			It("returns true if updated time is past expiration time", func() {
				backendDetails := routingtable.BackendServerDetails{TTL: 1, UpdatedTime: now.Add(-2 * time.Second)}
				Expect(backendDetails.Expired(defaultTTL)).To(BeTrue())
			})

			It("returns false if updated time is not past expiration time", func() {
				backendDetails := routingtable.BackendServerDetails{TTL: 1, UpdatedTime: now}
				Expect(backendDetails.Expired(defaultTTL)).To(BeFalse())
			})
		})

		Context("when backend details do not have TTL", func() {
			It("returns true if updated time is past expiration time", func() {
				backendDetails := routingtable.BackendServerDetails{TTL: 0, UpdatedTime: now.Add(-25 * time.Second)}
				Expect(backendDetails.Expired(defaultTTL)).To(BeTrue())
			})

			It("returns false if updated time is not past expiration time", func() {
				backendDetails := routingtable.BackendServerDetails{TTL: 0, UpdatedTime: now}
				Expect(backendDetails.Expired(defaultTTL)).To(BeFalse())
			})
		})
	})

	Describe("Entry", func() {
		var (
			routingTableEntry routingtable.Entry
			defaultTTL        time.Duration
		)

		BeforeEach(func() {
			backendServerKey := routingtable.BackendServerKey{Address: "some-ip-1", Port: 1234}
			backendServerDetails := &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: time.Now().Add(-10 * time.Second)}
			backendServerKey2 := routingtable.BackendServerKey{Address: "some-ip-2", Port: 1235}
			backendServerDetails2 := &routingtable.BackendServerDetails{ModificationTag: modificationTag, UpdatedTime: time.Now()}
			backends := map[routingtable.BackendServerKey]*routingtable.BackendServerDetails{
				backendServerKey:  backendServerDetails,
				backendServerKey2: backendServerDetails2,
			}
			routingTableEntry = routingtable.Entry{Backends: backends}
		})

		JustBeforeEach(func() {
			routingTableEntry.PruneBackends(defaultTTL)
		})

		Context("when it has expired backends", func() {
			BeforeEach(func() {
				defaultTTL = 5
			})

			It("prunes expired backends", func() {
				Expect(routingTableEntry.Backends).To(HaveLen(1))
			})
		})

		Context("when it does not have any expired backends", func() {
			BeforeEach(func() {
				defaultTTL = 15
			})

			It("prunes expired backends", func() {
				Expect(routingTableEntry.Backends).To(HaveLen(2))
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

func compareEntries(table *routingtable.RoutingTable, key routingtable.RoutingKey, newEntry routingtable.Entry) bool {
	existingEntry, ok := table.Entries[key]
	if ok == true && reflect.DeepEqual(existingEntry, newEntry) {
		return false
	}
	table.Entries[key] = newEntry
	return true
}
