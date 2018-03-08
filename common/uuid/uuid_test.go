/*
 * Portions Copyright (c) 2017,2018, F5 Networks, Inc.
 */

package uuid_test

import (
	"github.com/F5Networks/cf-bigip-ctlr/common/uuid"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("UUID", func() {
	It("creates a uuid", func() {
		uuid, err := uuid.GenerateUUID()
		Expect(err).ToNot(HaveOccurred())
		Expect(uuid).To(HaveLen(36))
	})
})
