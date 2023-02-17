package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/trace"
	"gopkg.in/yaml.v2"
)

var (
	configFile = flag.String("config_file", "hub2prom.yaml", "configuration `filename`")
	port       = flag.Int("port", 0, "port to run on")
)

type Config struct {
	MakerAPI    string `yaml:"maker_api"` // http://<ip>/apps/api/<n>/devices
	AccessToken string `yaml:"access_token"`

	Metrics []string `yaml:"metrics"`
}

func main() {
	flag.Parse()

	trace.AuthRequest = func(req *http.Request) (any, sensitive bool) {
		return true, true
	}

	var config Config
	configRaw, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Reading config file %s: %v", *configFile, err)
	}
	if err := yaml.UnmarshalStrict(configRaw, &config); err != nil {
		log.Fatalf("Parsing config from %s: %v", *configFile, err)
	}

	coll := newHubCollector(config)
	prometheus.MustRegister(coll)

	http.Handle("/metrics", promhttp.Handler())

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

// hubCollector implements prometheus.Collector by scraping a Hubitat.
type hubCollector struct {
	apiBase     string // will not have trailing slash
	accessToken string

	gauges map[string]*prometheus.GaugeVec
}

func newHubCollector(cfg Config) *hubCollector {
	hc := &hubCollector{
		apiBase:     strings.TrimSuffix(cfg.MakerAPI, "/"),
		accessToken: cfg.AccessToken,

		gauges: make(map[string]*prometheus.GaugeVec),
	}
	for _, m := range cfg.Metrics {
		hc.gauges[m] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: m,
			// TODO: Include Help
		}, []string{"name", "label", "room"})
	}
	return hc
}

func (hc *hubCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, gauge := range hc.gauges {
		gauge.Describe(ch)
	}
}

func (hc *hubCollector) Collect(ch chan<- prometheus.Metric) {
	tr := trace.New("hubCollector.Collect", "")
	defer tr.Finish()

	allURL := hc.apiBase + "/all?access_token="
	tr.LazyPrintf("Scraping %s<redacted>", allURL)
	resp, err := http.Get(allURL + url.QueryEscape(hc.accessToken))
	if err != nil {
		tr.LazyPrintf("Request failed: %v", err)
		tr.SetError()
		return
	}
	if resp.StatusCode != 200 {
		tr.LazyPrintf("Non-200 HTTP status: %s", resp.Status)
		tr.SetError()
		return
	}
	encBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		tr.LazyPrintf("Reading response body: %v", err)
		tr.SetError()
		return
	}
	tr.LazyPrintf("%d JSON bytes returned", len(encBody))

	var all allDevices
	if err := json.Unmarshal(encBody, &all); err != nil {
		tr.LazyPrintf("Parsing JSON of response body: %v", err)
		tr.SetError()
		return
	}
	tr.LazyPrintf("Parsed info for %d devices", len(all))

	// Clear old data to permit devices to drop.
	for _, gauge := range hc.gauges {
		gauge.Reset()
	}

	for _, dev := range all {
		labelSet := prometheus.Labels{
			"name":  dev.Name,
			"label": dev.Label,
			"room":  dev.Room,
		}
		for attr, rawValue := range dev.Attributes {
			gaugeVec, ok := hc.gauges[attr]
			if !ok {
				// Metric not enabled.
				continue
			}
			gauge := gaugeVec.With(labelSet)

			// Most attributes are floating point numbers.
			// Some are not and get special handling.
			rawStr, ok := rawValue.(string)
			if !ok {
				// TODO: Log a warning? Seems like `null` is the only real situation that hits this.
				continue
			}
			switch attr {
			default:
				value, err := strconv.ParseFloat(rawStr, 64)
				if err != nil {
					tr.LazyPrintf("Bad float attribute %q=%q for %q: %v", attr, rawValue, dev.Label, err)
					tr.SetError()
					continue // keep going anyway
				}
				gauge.Set(value)
			case "motion":
				switch rawStr {
				case "inactive":
					gauge.Set(0)
				case "active":
					gauge.Set(1)
				default:
					tr.LazyPrintf("Unknown motion=%q for %q", rawValue, dev.Label)
					tr.SetError()
					continue // keep going anyway
				}
			case "contact":
				switch rawStr {
				case "closed":
					gauge.Set(0)
				case "open":
					gauge.Set(1)
				default:
					tr.LazyPrintf("Unknown contact=%q for %q", rawValue, dev.Label)
					tr.SetError()
					continue // keep going anyway
				}
			}
		}
	}

	for _, gauge := range hc.gauges {
		gauge.Collect(ch)
	}
}

// allDevices represents the JSON returned by the Hubitat Maker API for fetching
// information about all devices with full details.
type allDevices []Device

type Device struct {
	Name       string                 `json:"name"`  // per manufacturer e.g. "Aeotec AerQ"
	Label      string                 `json:"label"` // user controllable e.g. "Living Room T&H Sensor"
	Room       string                 `json:"room"`  // user controllable e.g. "Living Room" (nullable?)
	Attributes map[string]interface{} `json:"attributes"`
}
