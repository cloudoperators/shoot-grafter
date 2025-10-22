package shoot_test

import (
	"testing"

	"shoot-grafter/internal/test"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestShootController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ShootControllerSuite")
}

var _ = BeforeSuite(func() {
	test.TestBeforeSuite()

})

var _ = AfterSuite(func() {
	test.TestAfterSuite()
})
