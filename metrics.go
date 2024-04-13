package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var metricTemp = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "temperature_f",
}, []string{"address", "name", "display_name"})

var metricBattery = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "battery_mv",
}, []string{"address", "name", "display_name"})

var metricLastConnectionTime = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "ble_sensor_last_connection_time_s",
}, []string{"address", "name", "display_name"})
