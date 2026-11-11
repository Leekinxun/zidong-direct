package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

var reservedImportNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

var importIPv6Global = netip.MustParsePrefix("2000::/3")

func publicImportAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !importIPv6Global.Contains(ip)) {
		return false
	}
	for _, reserved := range reservedImportNetworks {
		if reserved.Contains(ip) {
			return false
		}
	}
	return true
}

func validateImportURL(u *url.URL) error {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		u.User != nil || u.Fragment != "" || strings.Contains(u.Hostname(), "%") {
		return errors.New("请输入不含账号密码的公开 HTTP 或 HTTPS 文件地址")
	}
	if port := u.Port(); port != "" &&
		!(u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443") {
		return errors.New("文件 URL 仅支持 HTTP 和 HTTPS 的标准端口")
	}
	return nil
}

func dialImportURL(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range addrs {
		if !publicImportAddress(ip) {
			return nil, errors.New("文件 URL 必须指向公开网络地址")
		}
	}
	dialer := net.Dialer{Timeout: 6 * time.Second}
	for _, ip := range addrs {
		// Use the checked numeric address directly and retain the URL hostname
		// for TLS verification and HTTP Host.
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("无法连接文件 URL")
}

type temporaryImportFile struct {
	*os.File
}

func (file *temporaryImportFile) Close() error {
	return errors.Join(file.File.Close(), os.Remove(file.Name()))
}

func openImportURL(ctx context.Context, rawURL string, maxBytes int64) (importFile, int64, error) {
	source, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, 0, errors.New("文件 URL 格式无效")
	}
	if err := validateImportURL(source); err != nil {
		return nil, 0, err
	}
	if maxBytes <= 0 || maxBytes == 1<<63-1 {
		return nil, 0, errors.New("文件 URL 大小限制无效")
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           dialImportURL,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("文件 URL 重定向次数过多")
			}
			if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return errors.New("HTTPS 文件 URL 不能跳转到 HTTP")
			}
			req.Header.Del("Referer")
			return validateImportURL(req.URL)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return nil, 0, errors.New("无法建立文件 URL 请求")
	}
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, 0, fmt.Errorf("无法下载文件: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("文件 URL 返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return nil, 0, errors.New("URL 文件超过导入大小限制")
	}
	local, err := os.CreateTemp("", "zidong-import-*")
	if err != nil {
		return nil, 0, err
	}
	file := &temporaryImportFile{File: local}
	size, err := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || size > maxBytes {
		_ = file.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("读取 URL 文件失败: %w", err)
		}
		return nil, 0, errors.New("URL 文件超过导入大小限制")
	}
	return file, size, nil
}
