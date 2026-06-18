package bridge

import (
	"bytes"
	"context"
	"fmt"

	_ "github.com/xtls/xray-core/main/distro/all"

	"github.com/xtls/xray-core/core"
)

func Run(ctx context.Context, cfg Config) error {
	data, err := JSONConfig(cfg)
	if err != nil {
		return err
	}
	c, err := core.LoadConfig("json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("load xray config: %w", err)
	}
	server, err := core.New(c)
	if err != nil {
		return fmt.Errorf("create xray server: %w", err)
	}
	if err := server.Start(); err != nil {
		_ = server.Close()
		return fmt.Errorf("start xray server: %w", err)
	}
	defer server.Close()

	<-ctx.Done()
	return nil
}
