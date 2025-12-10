package httpclient

import (
	"net/http"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/project-ai-services/ai-services/tests/e2e/utils"
)

var _ = ginkgo.Describe("HTTP Client", func() {
	ginkgo.It("can GET and parse JSON", func() {
		client := NewHttpClient()

		resp, err := client.Get("/health")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

		var data map[string]interface{}
		err = utils.DecodeJSON(resp.Body, &data)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})
