package controllers

import (
	apiv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"testing"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multitenancy Controllers Suite")
}

var _ = BeforeSuite(func() {
	Expect(apiv1alpha1.Install(scheme.Scheme)).To(Succeed())
})
