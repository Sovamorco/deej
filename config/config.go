package config

import (
	"context"

	"github.com/joomcode/errorx"
	"github.com/sovamorco/gommon/config"
)

type Config struct {
	SliderMapping [][]string `mapstructure:"slider_mapping"`
	SerialPort    string     `mapstructure:"serial_port"`
	BaudRate      int        `mapstructure:"baud_rate"`
}

func Load(ctx context.Context, filename string) (*Config, error) {
	var c Config

	err := config.LoadConfig(ctx, filename, &c)
	if err != nil {
		return nil, errorx.Decorate(err, "load config")
	}

	return &c, nil
}
