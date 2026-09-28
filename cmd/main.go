package main

import (
	"flag"
	"fmt"
	"interestBar/cmd/apps"
	"net/http"
	"os"
	"time"
)

func main() {
	var config, bootstrap, probe string
	flag.StringVar(&config, "c", "configs/config.yaml", "本地兜底配置文件(Nacos 不可用时使用)。")
	flag.StringVar(&bootstrap, "b", "configs/bootstrap.yaml", "Nacos 引导文件(含服务端地址/鉴权/环境映射)。不存在或为空时跳过 Nacos，直接加载 -c。")
	flag.StringVar(&probe, "probe", "", "探针模式：GET 该 URL，2xx 退出码 0，否则 1（供 distroless 镜像的 Docker HEALTHCHECK 使用，如 http://127.0.0.1:8888/healthz）。")
	flag.Parse()

	if probe != "" {
		os.Exit(runProbe(probe))
	}
	apps.Run(config, bootstrap)
}

// runProbe 探针模式：不加载配置、不连依赖，只做一次 HTTP GET。
func runProbe(url string) int {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "probe failed:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		fmt.Fprintln(os.Stderr, "probe failed: status", resp.StatusCode)
		return 1
	}
	return 0
}
