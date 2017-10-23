/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package stats_test

import (
	"fmt"
	"math/rand"
	"time"

	. "github.com/F5Networks/cf-bigip-ctlr/stats"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ActiveApps", func() {
	var activeApps *ActiveApps

	BeforeEach(func() {
		activeApps = NewActiveApps()
	})

	It("marks application ids active", func() {
		activeApps.Mark("a", time.Unix(1, 0))
		apps := activeApps.ActiveSince(time.Unix(1, 0))
		Expect(apps).To(HaveLen(1))
	})

	It("marks existing applications", func() {
		activeApps.Mark("b", time.Unix(1, 0))
		apps := activeApps.ActiveSince(time.Unix(1, 0))
		Expect(apps).To(HaveLen(1))

		activeApps.Mark("b", time.Unix(2, 0))
		apps = activeApps.ActiveSince(time.Unix(1, 0))
		Expect(apps).To(HaveLen(1))
	})

	It("trims aging application ids", func() {
		for i, x := range []string{"a", "b", "c"} {
			activeApps.Mark(x, time.Unix(int64(i+1), 0))
		}
		apps := activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(3))

		activeApps.Trim(time.Unix(1, 0))
		apps = activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(2))

		activeApps.Trim(time.Unix(2, 0))
		apps = activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(1))

		activeApps.Trim(time.Unix(3, 0))
		apps = activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(0))
	})

	It("returns application ids active since a point in time", func() {
		activeApps.Mark("a", time.Unix(1, 0))
		Expect(activeApps.ActiveSince(time.Unix(1, 0))).To(Equal([]string{"a"}))
		Expect(activeApps.ActiveSince(time.Unix(3, 0))).To(Equal([]string{}))
		Expect(activeApps.ActiveSince(time.Unix(5, 0))).To(Equal([]string{}))

		activeApps.Mark("b", time.Unix(3, 0))
		Expect(activeApps.ActiveSince(time.Unix(1, 0))).To(Equal([]string{"b", "a"}))
		Expect(activeApps.ActiveSince(time.Unix(3, 0))).To(Equal([]string{"b"}))
		Expect(activeApps.ActiveSince(time.Unix(5, 0))).To(Equal([]string{}))
	})

	benchmarkMark := func(b Benchmarker, apps int) {
		var i int

		x := make([]string, 0)
		for i = 0; i < apps; i++ {
			x = append(x, fmt.Sprintf("%d", i))
		}

		b.Time(fmt.Sprintf("Mark %d application ids", apps), func() {
			for i = 0; i < apps; i++ {
				activeApps.Mark(x[rand.Intn(len(x))], time.Unix(int64(i), 0))
			}
		})
	}

	Measure("Mark performance", func(b Benchmarker) {
		benchmarkMark(b, 10)
		benchmarkMark(b, 100)
		benchmarkMark(b, 1000)
		benchmarkMark(b, 10000)
	}, 5)
})
