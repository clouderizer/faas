package handlers

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"encoding/json"
    "net/http"

	"github.com/openfaas/faas/gateway/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// HTTPNotifier notify about HTTP request/response
type HTTPNotifier interface {
	Notify(method string, URL string, originalURL string, statusCode int, event string, duration time.Duration)
}

// PrometheusServiceNotifier notifier for core service endpoints
type PrometheusServiceNotifier struct {
	ServiceMetrics *metrics.ServiceMetricOptions
}

// Notify about service metrics
func (psn PrometheusServiceNotifier) Notify(method string, URL string, originalURL string, statusCode int, event string, duration time.Duration) {
	code := fmt.Sprintf("%d", statusCode)
	path := urlToLabel(URL)

	psn.ServiceMetrics.Counter.WithLabelValues(method, path, code).Inc()
	psn.ServiceMetrics.Histogram.WithLabelValues(method, path, code).Observe(duration.Seconds())
}

func urlToLabel(path string) string {
	if len(path) > 0 {
		path = strings.TrimRight(path, "/")
	}
	if path == "" {
		path = "/"
	}
	return path
}

// PrometheusFunctionNotifier records metrics to Prometheus
type PrometheusFunctionNotifier struct {
	Metrics *metrics.MetricOptions
	//FunctionNamespace default namespace of the function
	FunctionNamespace string
}

// Notify records metrics in Prometheus
func (p PrometheusFunctionNotifier) Notify(method string, URL string, originalURL string, statusCode int, event string, duration time.Duration) {
	serviceName := getServiceName(originalURL)
	infraType := fetchInfraType(serviceName)
	if len(p.FunctionNamespace) > 0 {
		if !strings.Contains(serviceName, ".") {
			serviceName = fmt.Sprintf("%s.%s", serviceName, p.FunctionNamespace)
		}
	}

	if event == "completed" {
		seconds := duration.Seconds()
		p.Metrics.GatewayFunctionsHistogram.
			// WithLabelValues(serviceName).
			With(prometheus.Labels{"function_name": serviceName, "infra_type": infraType}).
			Observe(seconds)

		code := strconv.Itoa(statusCode)

		p.Metrics.GatewayFunctionInvocation.
			With(prometheus.Labels{"function_name": serviceName, "code": code}).
			Inc()
	} else if event == "started" {
		p.Metrics.GatewayFunctionInvocationStarted.WithLabelValues(serviceName).Inc()
	}

}

func getServiceName(urlValue string) string {
	var serviceName string
	forward := "/function/"
	if strings.HasPrefix(urlValue, forward) {
		// With a path like `/function/xyz/rest/of/path?q=a`, the service
		// name we wish to locate is just the `xyz` portion.  With a positive
		// match on the regex below, it will return a three-element slice.
		// The item at index `0` is the same as `urlValue`, at `1`
		// will be the service name we need, and at `2` the rest of the path.
		matcher := functionMatcher.Copy()
		matches := matcher.FindStringSubmatch(urlValue)
		if len(matches) == hasPathCount {
			serviceName = matches[nameIndex]
		}
	}
	return strings.Trim(serviceName, "/")
}

func fetchInfraType(service string) string {
	passwd, pwdErr := ioutil.ReadFile("/var/secrets/basic-auth-password")
	if pwdErr != nil {
		fmt.Println("unable to load pwd file")
		return ""
	}
	username, userErr := ioutil.ReadFile("/var/secrets/basic-auth-user")
	if userErr != nil {
		fmt.Println("unable to load user file")
		return ""
	}
	client := &http.Client{}
	req, err := http.NewRequest("GET", os.Getenv("serverlessurl") + "/system/function/"+service, nil)
	req.SetBasicAuth(strings.TrimSpace(string(username)), strings.TrimSpace(string(passwd)))
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
		return ""
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
		return ""
	}

	sb := string(body)
	var jsonb map[string]interface{}
	var c map[string]interface{}
	json.Unmarshal([]byte(sb), &jsonb)
	//c = jsonb["annotations"].(map[string]interface{})
	if f, found := jsonb["annotations"]; found {
		c = f.(map[string]interface{})
		if d, found := c["prometheus_labels"]; found {
			b := d.(string)
			return b
		}
	}
	return ""
	// b := jsonb["annotations"].(map[string]interface{})["prometheus_labels"].(string)

}

// LoggingNotifier notifies a log about a request
type LoggingNotifier struct {
}

// Notify the LoggingNotifier about a request
func (LoggingNotifier) Notify(method string, URL string, originalURL string, statusCode int, event string, duration time.Duration) {
	if event == "completed" {
		log.Printf("Forwarded [%s] to %s - [%d] - %fs seconds", method, originalURL, statusCode, duration.Seconds())
	}
}