package unlock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RequestContext 是上层业务已知的认证发起上下文。字段可以为空；Run 会补齐
// 当前进程、时间与调用栈。这里绝不记录命令行和环境变量，避免把 token 带进页面。
type RequestContext struct {
	Source         string
	Facade         string
	ClientID       string
	Operation      string
	OperationID    string
	ProjectRoot    string
	WorkspacePlane string
}

// RequestDetails 是展示在 localhost 解锁页上的诊断信息。
type RequestDetails struct {
	ID             string
	Source         string
	Facade         string
	ClientID       string
	Operation      string
	OperationID    string
	ProjectRoot    string
	WorkspacePlane string
	RequestedAt    string
	Executable     string
	ProcessName    string
	PID            int
	PPID           int
	ParentProcess  string
	WorkingDir     string
	Hostname       string
	IPs            string
	MACs           string
	GoVersion      string
	CallStack      string
}

func captureRequestDetails(in RequestContext) RequestDetails {
	exe, _ := os.Executable()
	exe, _ = filepath.Abs(exe)
	cwd, _ := os.Getwd()
	hostname, ips, macs := captureHostNetwork()
	return RequestDetails{
		ID:             newRequestID(),
		Source:         fallback(strings.TrimSpace(in.Source), "未标注"),
		Facade:         fallback(strings.TrimSpace(in.Facade), "未知（可能为进程内直接调用）"),
		ClientID:       fallback(strings.TrimSpace(in.ClientID), "未知"),
		Operation:      fallback(strings.TrimSpace(in.Operation), "未知"),
		OperationID:    fallback(strings.TrimSpace(in.OperationID), "未知"),
		ProjectRoot:    fallback(strings.TrimSpace(in.ProjectRoot), "未提供"),
		WorkspacePlane: fallback(strings.TrimSpace(in.WorkspacePlane), "未知"),
		RequestedAt:    time.Now().Format(time.RFC3339Nano),
		Executable:     fallback(exe, "unknown"),
		ProcessName:    fallback(filepath.Base(exe), "unknown"),
		PID:            os.Getpid(),
		PPID:           os.Getppid(),
		ParentProcess:  fallback(parentProcessPath(os.Getppid()), "无法读取"),
		WorkingDir:     fallback(cwd, "unknown"),
		Hostname:       hostname,
		IPs:            ips,
		MACs:           macs,
		GoVersion:      runtime.Version(),
		CallStack:      captureCallStack(),
	}
}

func captureHostNetwork() (hostname, ips, macs string) {
	hostname = fallback(osHostname(), "无法读取")
	ifaces, err := net.Interfaces()
	if err != nil {
		return hostname, "无法读取", "无法读取"
	}
	var ipList, macList []string
	seenIP := map[string]struct{}{}
	seenMAC := map[string]struct{}{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.TrimSpace(iface.Name)
		if mac := formatHardwareAddr(iface.HardwareAddr); mac != "" {
			if _, ok := seenMAC[mac]; !ok {
				seenMAC[mac] = struct{}{}
				if name != "" {
					macList = append(macList, mac+" ("+name+")")
				} else {
					macList = append(macList, mac)
				}
			}
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			s := ip.String()
			if _, ok := seenIP[s]; ok {
				continue
			}
			seenIP[s] = struct{}{}
			if name != "" {
				ipList = append(ipList, s+" ("+name+")")
			} else {
				ipList = append(ipList, s)
			}
		}
	}
	return hostname, fallback(strings.Join(ipList, "; "), "无可用地址"), fallback(strings.Join(macList, "; "), "无可用地址")
}

func osHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func formatHardwareAddr(addr net.HardwareAddr) string {
	if len(addr) == 0 {
		return ""
	}
	for _, b := range addr {
		if b != 0 {
			return strings.ToUpper(addr.String())
		}
	}
	return ""
}

func newRequestID() string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("auth-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("auth-%d-%s", os.Getpid(), hex.EncodeToString(suffix[:]))
}

func captureCallStack() string {
	pcs := make([]uintptr, 40)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	lines := make([]string, 0, n)
	for {
		frame, more := frames.Next()
		if !strings.HasPrefix(frame.Function, "runtime.") {
			lines = append(lines, fmt.Sprintf("%s\n    %s:%d", frame.Function, frame.File, frame.Line))
		}
		if !more {
			break
		}
	}
	if len(lines) == 0 {
		return "调用栈不可用"
	}
	return strings.Join(lines, "\n")
}

func fallback(value, fallbackValue string) string {
	if value == "" {
		return fallbackValue
	}
	return value
}
