//go:build ignore

// E2E acceptance for the raw.firoyang.com compat plane, run from the
// workstation with a hosts override pointing raw.firoyang.com at the
// publish host. Simulates the OLDEST installed clients:
//   - cronkit: entry raw.../directory/cronkit.pb, channel stable, code 9
//   - dec:     entry raw.../directory/dec.pb,     channel dev,   code 1013089
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	rupv2 "github.com/shichao402/relkit/api/rup/v2"
	"github.com/shichao402/relkit/internal/envelope"
	"github.com/shichao402/relkit/internal/inprocess"
	"github.com/shichao402/relkit/internal/keys"
	"github.com/shichao402/relkit/sdk"
)

// dialPublish pins both the compat entry host and the store host to loopback
// when running on the publish host itself, and skips TLS verification (compat
// vhost borrows the publish cert until DNS cut + certbot).
func dialPublish(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, _ := net.SplitHostPort(addr)
	if host == "raw.firoyang.com" || host == "publish.firoyang.com" {
		host = "127.0.0.1"
	}
	d := &net.Dialer{Timeout: 15 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(host, port))
}

func run(product, entry, channel string, code int64, selectors map[string]string, lastSeenDir, lastSeenIdx *int64) {
	state := &sdk.UpdateState{
		LastSeenDirectorySequence: lastSeenDir,
		LastSeenSequence:          lastSeenIdx,
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: dialPublish,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // compat vhost borrows publish cert until DNS cut + certbot
			},
		},
	}
	fetcher := &sdk.HTTPFetcher{Client: client, DocumentTimeout: 30 * time.Second}
	u := &inprocess.Updater{
		Product:         product,
		Channel:         channel,
		CurrentCode:     int(code),
		EntryURLs:       []string{entry},
		TrustedKeys:     parseKeys(product),
		ClientSelectors: selectors,
		Fetcher:         fetcher,
		StateStore:      sdk.NewMemoryStateStore(state),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Debug: fetch the directory through the same fetcher and dump services.
	if dbg, err := fetcher.GetBytes(ctx, entry+"?debug=1"); err == nil {
		env2, eerr := rupv2.UnmarshalEnvelope(dbg)
		if eerr != nil {
			fmt.Printf("   debug dir unmarshal: %v\n", eerr)
		} else if doc2, derr := envelope.OpenDirectoryEnvelope(env2, parseKeys(product)); derr != nil {
			fmt.Printf("   debug dir verify: %v\n", derr)
		} else {
			fmt.Printf("   debug dir seq=%d services=%d\n", doc2.DirectorySequence, len(doc2.Services))
			for _, svc := range doc2.Services {
				fmt.Printf("     svc %s ch=%s -> %s\n", svc.Id, svc.Channel, svc.IndexUrl)
			}
		}
	} else {
		fmt.Printf("   debug directory fetch: %v\n", err)
	}

	res := u.CheckForce(ctx, true)
	fmt.Printf("== %s (channel %s, code %d)\n", product, channel, code)
	if res.Err != nil {
		fmt.Printf("   CHECK FAILED: %v\n", res.Err)
		fmt.Printf("   attempts: %v\n", res.Attempts)
		os.Exit(1)
	}
	if res.Throttled {
		fmt.Println("   throttled (unexpected with force)")
		os.Exit(1)
	}
	if res.UpToDate {
		fmt.Printf("   up-to-date (seq %d) — old client would see NO update\n", res.Sequence)
		return
	}
	if res.Available == nil {
		fmt.Printf("   no update available (fallback? %+v)\n", res.Fallback)
		return
	}
	a := res.Available
	fmt.Printf("   update available: %s (code %d, seq %d, hops %d)\n", a.Target.Version, a.Target.Code, a.Sequence, a.RemainingHops)
	fmt.Printf("   artifact: %s (%d bytes) %d mirror(s)\n", a.Artifact.Id, a.Artifact.Size, len(a.Artifact.Urls))
	for _, u := range a.Artifact.Urls {
		fmt.Printf("     mirror: %s\n", u)
	}
	// download first 1 byte via probe to validate URL accessibility
	probe, err := fetcher.Probe(ctx, a.Artifact.Urls[0])
	if err != nil {
		fmt.Printf("   PROBE FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   artifact probe: status %d, len %d, ranges %v\n", probe.StatusCode, probe.ContentLength, probe.AcceptRanges)
}

func parseKeys(product string) sdk.TrustedKeys {
	// Reuse the same base64 values embedded in clients.
	b64 := map[string]string{
		"cronkit": "iDVCC4DBw1weJn8gl+D82ap3j+ueHV/OksSdlt2SGvk=",
		"dec":     "zVkesjz/3BhLrZ9qCvSJN0OdrIsePL4+v6AI9CCtio4=",
	}
	id := map[string]string{
		"cronkit": "cronkit-2026",
		"dec":     "dec-2026",
	}
	raw, err := keys.DecodePublicKey(b64[product], "e2e")
	if err != nil {
		panic(err)
	}
	return sdk.TrustedKeys{id[product]: raw}
}

func main() {
	// Oldest cronkit stable client: saw COS directory seq 11, index seq 4.
	run("cronkit", "https://raw.firoyang.com/rup/directory/cronkit.pb", "stable", 9,
		map[string]string{"os": "windows", "arch": "x64", "apply": "relkit-payload"},
		ptr(11), ptr(4))
	// Oldest dec dev client: saw COS directory seq 30, index seq 8.
	run("dec", "https://raw.firoyang.com/rup/directory/dec.pb", "dev", 1013089,
		map[string]string{"os": "darwin", "arch": "amd64", "audience": "runtime", "component": "dec-exec", "apply": "relkit-payload"},
		ptr(30), ptr(8))
	fmt.Println("ALL PASS")
}

func ptr(v int64) *int64 { return &v }
