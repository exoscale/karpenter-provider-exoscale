package scenarios_test

import (
	"testing"

	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestScenarios(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Scenarios Suite")
}

var _ = BeforeSuite(func() {
	framework.SetupSuite()
})

var _ = AfterSuite(func() {
	framework.TeardownSuite()
})
