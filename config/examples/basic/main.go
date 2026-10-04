package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/bkcarlos/goparts/config"
)

type AppConfig struct {
	Name    string        `json:"name" yaml:"name" default:"demo" env:"NAME" required:"true"`
	Port    int           `json:"port" yaml:"port" default:"8080" env:"PORT"`
	Timeout time.Duration `json:"timeout" yaml:"timeout" default:"5s" env:"TIMEOUT"`
}

func (c *AppConfig) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if c.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}

func main() {
	path := flag.String("config", "", "optional JSON or YAML config file")
	flag.Parse()
	cfg, err := config.Load[AppConfig](config.Options{File: *path, EnvPrefix: "DEMO_"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("name=%s port=%d timeout=%s\n", cfg.Name, cfg.Port, cfg.Timeout)
}
