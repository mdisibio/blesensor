package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
	"tinygo.org/x/bluetooth"
)

var adapter = bluetooth.DefaultAdapter

func main() {
	if len(os.Args) == 1 {
		fmt.Println("Command must be one of: -scan, -cli, or -poll")
		return
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-scan":
			err := scan()
			if err != nil {
				panic(err)
			}
		case "-cli":
			err := cli("7dae7cba-5e45-6a13-116d-5fccc1de2bea")
			if err != nil {
				panic(err)
			}
		case "-poll":
			cfg, err := loadConfig()
			if err != nil {
				panic(err)
			}

			if len(cfg.Devices) == 0 {
				panic("Must configure at least one device in config.yaml")
			}

			serveMetrics(9090)

			err = adapter.Enable()
			if err != nil {
				panic(err)
			}

			for _, d := range cfg.Devices {
				go poll(d)
			}
			fmt.Scanln()
		default:
			fmt.Println("Unknown command:", os.Args[1])
		}
	}
}

func loadConfig() (Config, error) {
	b, err := os.ReadFile("config.yaml")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{}
	err = yaml.Unmarshal(b, &cfg)
	return cfg, err
}

func serveMetrics(port int) {
	http.Handle("/metrics", promhttp.Handler())
	addr := fmt.Sprintf(":%d", port)
	fmt.Println("Serving http at", addr)
	go func() {
		http.ListenAndServe(addr, nil)
	}()
}

func cli(addr string) error {
	fmt.Println("cli connecting to", addr)

	err := adapter.Enable()
	if err != nil {
		return fmt.Errorf("enable adapter: %w", err)
	}

	deviceAddr := &bluetooth.Address{}
	deviceAddr.Set(addr)
	dev, err := adapter.Connect(*deviceAddr, bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(time.Minute),
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	println(dev)

	println("discovering services...")
	svcs, err := dev.DiscoverServices(nil)
	if err != nil {
		return fmt.Errorf("discover services: %w", err)
	}

	println("discovered:", svcs)
	var rx bluetooth.DeviceCharacteristic
	for _, svc := range svcs {
		println("Discovered service:", svc.String())

		if svc.UUID() == bluetooth.ServiceUUIDNordicUART {
			println("Discovering characteristics...")
			chars, err := svc.DiscoverCharacteristics(nil)
			if err != nil {
				return fmt.Errorf("discover characteristics: %w", err)
			}

			for _, char := range chars {
				fmt.Println("Discovered char", char.UUID().String())

				switch char.UUID() {
				case bluetooth.CharacteristicUUIDUARTTX:
					err = char.EnableNotifications(func(buf []byte) {
						fmt.Printf("Got bytes from tx: %s %X\n", buf, buf)
					})
					if err != nil {
						return fmt.Errorf("enable notification on uart tx: %w", err)
					}

				case bluetooth.CharacteristicUUIDUARTRX:
					rx = char
				}
			}
		}
	}

	fmt.Println("Press ctrl+c to exit")
	for {
		var command string
		_, err := fmt.Scanln(&command)
		if err == nil {
			_, err = rx.WriteWithoutResponse([]byte(command))
			if err != nil {
				return fmt.Errorf("write ble command: %w", err)
			}
		}
	}
}

func scan() error {
	fmt.Println("scanning")
	fmt.Println("Press ctrl+c to exit")

	err := adapter.Enable()
	if err != nil {
		return fmt.Errorf("enable adapter: %w", err)
	}

	found := map[string]struct{}{}
	err = adapter.Scan(func(adapter *bluetooth.Adapter, device bluetooth.ScanResult) {
		addr := device.Address.String()

		if _, ok := found[addr]; ok {
			// Already known
			return
		}

		if strings.HasPrefix(device.LocalName(), "C T") {
			println("found device:", device.Address.String(), device.RSSI, device.LocalName())
			found[addr] = struct{}{}
		}
	})
	return err
}

func poll(dev Device) {
	err := pollOnce(dev)
	if err != nil {
		fmt.Printf("poll %s: %v\n", dev.Name, err)
	}

	f := dev.Freq
	if f == 0 {
		f = time.Minute
	}

	// Now poll on the timer
	t := time.NewTicker(f)
	for range t.C {
		err = pollOnce(dev)
		if err != nil {
			fmt.Printf("poll %s: %v\n", dev.Name, err)
		}
	}
}

func pollOnce(d Device) error {
	var (
		addr                = d.Address
		name                = d.Name
		disp                = d.Disp
		mLastConnectionTime = metricLastConnectionTime.WithLabelValues(addr, name, disp)
		mBattery            = metricBattery.WithLabelValues(addr, name, disp)
		mTemp               = metricTemp.WithLabelValues(addr, name, disp)
	)

	deviceAddr := &bluetooth.Address{}
	deviceAddr.Set(addr)

	start := time.Now()
	dev, err := adapter.Connect(*deviceAddr, bluetooth.ConnectionParams{
		ConnectionTimeout: bluetooth.NewDuration(time.Minute),
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	mLastConnectionTime.Set(time.Since(start).Seconds())

	svcs, err := dev.DiscoverServices([]bluetooth.UUID{
		bluetooth.ServiceUUIDNordicUART,
	})
	if err != nil {
		return fmt.Errorf("discover services: %w", err)
	}

	svc := svcs[0]

	chars, err := svc.DiscoverCharacteristics([]bluetooth.UUID{
		bluetooth.CharacteristicUUIDUARTTX,
		bluetooth.CharacteristicUUIDUARTRX,
	})
	if err != nil {
		return fmt.Errorf("discover characteristics: %w", err)
	}

	var (
		tx = chars[0]
		rx = chars[1]
		mv int
		f  float64
	)

	err = tx.EnableNotifications(func(buf []byte) {
		if d, ok := parseInt(buf, "Battery voltage (mV): %d"); ok {
			mv = d
			mBattery.Set(float64(mv))
			return
		}

		if d, ok := parseInt(buf, "Temperature value (0.01 degC): %d"); ok {
			f = float64(d)*0.018 + 32
			mTemp.Set(f)
		}
	})
	if err != nil {
		return fmt.Errorf("enable notification on uart tx: %w", err)
	}

	// Send commands
	rx.WriteWithoutResponse([]byte("GET_BATT_VOLTAGE\n"))
	time.Sleep(100 * time.Millisecond)
	rx.WriteWithoutResponse([]byte("GET_SENSOR_DATA\n"))

	// Wait for responses
	time.Sleep(time.Second)

	fmt.Printf("[%s] %s   Temp: %.2f F  Battery: %d mV\n", time.Now().Format(time.DateTime), name, f, mv)

	err = dev.Disconnect()
	if err != nil {
		return fmt.Errorf("disconnect: %w", err)
	}
	return nil
}

func parseInt(input []byte, format string) (int, bool) {
	var d int
	if n, err := fmt.Sscanf(string(input), format, &d); n == 1 && err == nil {
		return d, true
	}
	return 0, false
}
