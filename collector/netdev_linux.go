// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build !nonetdev

package collector

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/log"
)

var (
	procNetDevFieldSep   = regexp.MustCompile("[ :] *")
	netdevIgnoredDevices = flag.String(
		"collector.netdev.ignored-devices", "^$",
		"Regexp of net devices to ignore for netdev collector.")
)

type netDevCollector struct {
	subsystem             string
	ignoredDevicesPattern *regexp.Regexp
	metricDescs           map[string]*prometheus.Desc
}

func init() {
	Factories["netdev"] = NewNetDevCollector
}

// Takes a prometheus registry and returns a new Collector exposing
// network device stats.
func NewNetDevCollector() (Collector, error) {
	pattern := regexp.MustCompile(*netdevIgnoredDevices)
	return &netDevCollector{
		subsystem:             "network",
		ignoredDevicesPattern: pattern,
		metricDescs:           map[string]*prometheus.Desc{},
	}, nil
}

func (c *netDevCollector) Update(ch chan<- prometheus.Metric) (err error) {
	netDev, err := getNetDevStats(c.ignoredDevicesPattern)
	if err != nil {
		return fmt.Errorf("couldn't get netstats: %s", err)
	}
	for dev, devStats := range netDev {
		for key, value := range devStats {
			desc, ok := c.metricDescs[key]
			if !ok {
				desc = prometheus.NewDesc(
					prometheus.BuildFQName(Namespace, c.subsystem, key),
					fmt.Sprintf("Network device statistic %s.", key),
					[]string{"device"},
					nil,
				)
				c.metricDescs[key] = desc
			}
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("invalid value %s in netstats: %s", value, err)
			}
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, dev)
		}
	}
	return nil
}

func getNetDevStats(ignore *regexp.Regexp) (map[string]map[string]string, error) {
	file, err := os.Open(procFilePath("net/dev"))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseNetDevStats(file, ignore)
}

func parseNetDevStats(r io.Reader, ignore *regexp.Regexp) (map[string]map[string]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Scan() // skip first header
	scanner.Scan()
	parts := strings.Split(string(scanner.Text()), "|")
	if len(parts) != 3 { // interface + receive + transmit
		return nil, fmt.Errorf("invalid header line in net/dev: %s",
			scanner.Text())
	}

	header := strings.Fields(parts[1])
	netDev := map[string]map[string]string{}
	for scanner.Scan() {
		line := strings.TrimLeft(string(scanner.Text()), " ")
		parts := procNetDevFieldSep.Split(line, -1)
		if len(parts) != 2*len(header)+1 {
			return nil, fmt.Errorf("invalid line in net/dev: %s", scanner.Text())
		}

		dev := parts[0][:len(parts[0])]
		if ignore.MatchString(dev) {
			log.Debugf("Ignoring device: %s", dev)
			continue
		}
		netDev[dev] = map[string]string{}
		for i, v := range header {
			netDev[dev]["receive_"+v] = parts[i+1]
			netDev[dev]["transmit_"+v] = parts[i+1+len(header)]
		}
	}
	return netDev, nil
}
