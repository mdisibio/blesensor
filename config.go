package main

import "time"

type Device struct {
	Name    string        `yaml:"name"`
	Address string        `yaml:"address"`
	Disp    string        `yaml:"display_name"`
	Orig    string        `yaml:"original_name"`
	Freq    time.Duration `yaml:"frequency"`
}

type Config struct {
	Devices []Device `yaml:"devices"`
}
