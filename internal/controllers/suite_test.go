package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
	"time"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multitenancy Controllers Suite")
	SetDefaultEventuallyTimeout(5 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
}
