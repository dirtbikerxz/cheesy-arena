// netgear_switch.go
// Minimal NETGEAR GS308E/EP automation:
// - Login per UI (md5(merge(password, rand)))
// - Reboot via dashboard hash -> POST /device_reboot.cgi
// - ICMP-only reachability & simple status machine (UNKNOWN, CONFIGURING, ACTIVE, ERROR)
// - Prints "changed from CONFIGURING to ACTIVE in Xs" after a reboot completes.

package network

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	pollPeriod       = 1 * time.Second
	needOK           = 3
	needFail         = 5
	defaultGrace     = 30 * time.Second
	httpTimeout      = 8 * time.Second
	waitReadyBudget  = 20 * time.Second
	maxDebugPeek     = 512 // only used in compact errors
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

type NetgearPlusSwitch struct {
	Name          string
	address       string
	plainPassword string

	// URLs
	origin    string
	loginURL  string
	indexURL  string
	rootURL   string
	dashURL   string
	rebootURL string

	// Public status (ALL CAPS)
	Status string

	// Monitoring
	lastRebootAt    time.Time
	rebootGrace     time.Duration
	consecutiveOK   int
	consecutiveFail int

	// HTTP/auth
	sid string // manual cookie (allows backslashes)
}

// NewNetgearPlusSwitch creates a switch with required fields only.
func NewNetgearPlusSwitch(name, address, password string) *NetgearPlusSwitch {
	base := "http://" + address
	return &NetgearPlusSwitch{
		Name:          name,
		address:       address,
		plainPassword: password,
		origin:        base,
		loginURL:      base + "/login.cgi",
		indexURL:      base + "/index.cgi",
		rootURL:       base + "/",
		dashURL:       base + "/dashboard.cgi",
		rebootURL:     base + "/device_reboot.cgi",
		Status:        "UNKNOWN",
		rebootGrace:   defaultGrace,
	}
}

// Run starts the background status monitor (ICMP).
func (s *NetgearPlusSwitch) Run(switchManagementEnabled bool) {
	for {
		time.Sleep(pollPeriod)
		if switchManagementEnabled {
			_ = s.updateMonitoring()
		}
	}
}

// Reboot logs in, fetches a fresh dashboard hash, POSTs reboot, and flips to CONFIGURING.
func (s *NetgearPlusSwitch) Reboot() {
	if !s.waitUntilReachable(waitReadyBudget) {
		s.Status = "ERROR"
		log.Printf("%s: ERROR - device unreachable before reboot attempt.", s.Name)
		return
	}

	if err := s.rebootWithLogin(); err != nil {
		s.Status = "ERROR"
		log.Printf("%s: ERROR - %v", s.Name, err)
		return
	}

	s.lastRebootAt = time.Now()
	s.Status = "CONFIGURING"
	log.Printf("%s: Reboot request sent - %s", s.Name, s.address)
}

// GetStatus returns the current status string (ALL CAPS).
func (s *NetgearPlusSwitch) GetStatus() string {
	return s.Status
}

// ---------------- Internals ----------------

func (s *NetgearPlusSwitch) updateMonitoring() error {
	reachable := s.pingICMP(1 * time.Second)
	if reachable {
		s.consecutiveOK++
		s.consecutiveFail = 0
		if s.consecutiveOK >= needOK && s.Status != "ACTIVE" {
			if s.Status == "CONFIGURING" && !s.lastRebootAt.IsZero() {
				d := time.Since(s.lastRebootAt).Round(time.Second)
				log.Printf("%s: changed from CONFIGURING to ACTIVE in %s.", s.Name, d.String())
			} else {
				log.Printf("%s: status changed from %s to ACTIVE.", s.Name, s.Status)
			}
			s.Status = "ACTIVE"
		}
		return nil
	}

	s.consecutiveFail++
	s.consecutiveOK = 0

	// During the grace window after a reboot, treat as CONFIGURING.
	if !s.lastRebootAt.IsZero() && time.Since(s.lastRebootAt) < s.rebootGrace {
		if s.Status != "CONFIGURING" {
			log.Printf("%s: status changed from %s to CONFIGURING.", s.Name, s.Status)
			s.Status = "CONFIGURING"
		}
		return nil
	}

	if s.consecutiveFail >= needFail && s.Status != "ERROR" {
		log.Printf("%s: status changed from %s to ERROR.", s.Name, s.Status)
		s.Status = "ERROR"
	}
	return nil
}

// pingICMP runs the platform ping once (no external deps).
func (s *NetgearPlusSwitch) pingICMP(timeout time.Duration) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// -n 1 (count 1), -w ms
		cmd = exec.Command("ping", "-n", "1", "-w", fmt.Sprintf("%d", int(timeout.Milliseconds())), s.address)
	default:
		// -c 1 (count 1), -W seconds
		sec := int(timeout / time.Second)
		if sec <= 0 {
			sec = 1
		}
		cmd = exec.Command("ping", "-c", "1", "-W", fmt.Sprintf("%d", sec), s.address)
	}
	return cmd.Run() == nil
}

func (s *NetgearPlusSwitch) waitUntilReachable(budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		if s.pingICMP(1 * time.Second) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *NetgearPlusSwitch) rebootWithLogin() error {
	client := &http.Client{Timeout: httpTimeout}

	// Light browser warmup + initial SID (some builds expect a SID cookie early).
	_, _ = s.fetchPage(client, s.loginURL, s.rootURL)
	_, _ = s.fetchPage(client, s.rootURL, s.rootURL)
	if s.sid == "" {
		s.sid = synthesizeSID()
	}

	// Fetch login page for rand, compute md5(merge(password, rand)).
	page, err := s.fetchPage(client, s.loginURL, s.rootURL)
	if err != nil {
		return fmt.Errorf("login prefetch: %w", err)
	}
	randStr := extractRand(page)
	pwHex := md5hex(mergeStrings(s.plainPassword, randStr))

	// POST /login.cgi then verify with /index.cgi (not redirecting back to /login.cgi).
	if ok, why, err := s.tryLoginOnce(client, pwHex); err != nil {
		return fmt.Errorf("login: %w", err)
	} else if !ok {
		return fmt.Errorf("login verify failed: %s", why)
	}

	// Fetch dashboard hash (XHR, no-cache), then POST to /device_reboot.cgi.
	hash := s.fetchDashboardHash(client)
	if hash == "" {
		return fmt.Errorf("no reboot hash from dashboard")
	}
	if err := s.postRebootConsiderRebooting(client, s.rebootURL, hash); err != nil {
		// If the device dropped right away, count as success.
		if !s.pingICMP(1*time.Second) || looksLikeConnectionDrop(err) || looksLikeTimeout(err) {
			return nil
		}
		return err
	}
	return nil
}

// --- HTTP helpers ---

func (s *NetgearPlusSwitch) fetchPage(client *http.Client, urlStr, referer string) ([]byte, error) {
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Origin", s.origin)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if s.sid != "" {
		req.Header.Set("Cookie", "SID="+s.sid)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	s.captureSID(resp)
	b, _ := io.ReadAll(resp.Body)
	return b, nil
}

func (s *NetgearPlusSwitch) tryLoginOnce(client *http.Client, pwHex string) (bool, string, error) {
	form := url.Values{}
	form.Set("password", pwHex)

	req, _ := http.NewRequest("POST", s.loginURL, bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", s.origin)
	req.Header.Set("Referer", s.loginURL)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	if s.sid != "" {
		req.Header.Set("Cookie", "SID="+s.sid)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "transport error", err
	}
	s.captureSID(resp)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Verify with GET /index.cgi
	req2, _ := http.NewRequest("GET", s.indexURL, nil)
	req2.Header.Set("User-Agent", defaultUserAgent)
	req2.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req2.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req2.Header.Set("Upgrade-Insecure-Requests", "1")
	req2.Header.Set("DNT", "1")
	req2.Header.Set("Connection", "keep-alive")
	req2.Header.Set("Origin", s.origin)
	req2.Header.Set("Referer", s.loginURL)
	if s.sid != "" {
		req2.Header.Set("Cookie", "SID="+s.sid)
	}

	resp2, err := client.Do(req2)
	if err != nil {
		return false, fmt.Sprintf("GET index.cgi error: %v", err), nil
	}
	defer resp2.Body.Close()
	s.captureSID(resp2)
	body, _ := io.ReadAll(resp2.Body)

	// If the page still tries to send you to /login.cgi, auth failed.
	if bytesContainsFold(body, []byte(`/login.cgi`)) || bytesContainsFold(body, []byte(`Redirect to Login`)) {
		return false, "redirected to login", nil
	}
	return true, "OK", nil
}

var (
	rxRand      = regexp.MustCompile(`(?i)<input[^>]+id=["']rand["'][^>]*value=["']([^"']+)["']`)
	rxHashKeyed = regexp.MustCompile(`(?i)\bhash\b[^0-9a-fA-F]*([0-9a-fA-F]{32})`)
	rxAny32Hex  = regexp.MustCompile(`\b[0-9a-fA-F]{32}\b`)
)

func extractRand(page []byte) string {
	if m := rxRand.FindSubmatch(page); len(m) >= 2 {
		return string(m[1])
	}
	return ""
}

func (s *NetgearPlusSwitch) fetchDashboardHash(client *http.Client) string {
	req, _ := http.NewRequest("GET", s.dashURL, nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Referer", s.indexURL)
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	if s.sid != "" {
		req.Header.Set("Cookie", "SID="+s.sid)
	}

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	s.captureSID(resp)
	b, _ := io.ReadAll(resp.Body)

	if m := rxHashKeyed.FindSubmatch(b); len(m) >= 2 {
		return strings.ToLower(string(m[1]))
	}
	if m := rxAny32Hex.FindSubmatch(b); len(m) >= 1 {
		return strings.ToLower(string(m[0]))
	}
	return ""
}

func (s *NetgearPlusSwitch) postRebootConsiderRebooting(client *http.Client, ep, hash string) error {
	form := []byte("hash=" + hash)

	req, _ := http.NewRequest("POST", ep, bytes.NewBuffer(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", s.origin)
	req.Header.Set("Referer", s.indexURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	if s.sid != "" {
		req.Header.Set("Cookie", "SID="+s.sid)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Often means reboot started and the socket died; accept as success.
		if looksLikeConnectionDrop(err) || looksLikeTimeout(err) {
			return nil
		}
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode/100 == 2 || resp.StatusCode/100 == 3 {
		return nil
	}
	// One last heuristic: if it went down immediately after, call it success.
	if !s.pingICMP(1 * time.Second) {
		return nil
	}
	return fmt.Errorf("reboot failed: %s", resp.Status)
}

// ---------------- small utils ----------------

func (s *NetgearPlusSwitch) captureSID(resp *http.Response) {
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if idx := strings.Index(sc, "SID="); idx >= 0 {
			val := sc[idx+4:]
			if semi := strings.IndexByte(val, ';'); semi >= 0 {
				val = val[:semi]
			}
			val = strings.TrimSpace(val)
			if val != "" && val != s.sid {
				s.sid = val
				return
			}
		}
	}
}

func synthesizeSID() string {
	alpha := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789[]^_-`\\"
	length := 72
	buf := make([]byte, length)
	for i := 0; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alpha))))
		buf[i] = alpha[n.Int64()]
	}
	return string(buf)
}

func mergeStrings(a, b string) string {
	ra := []rune(a)
	rb := []rune(b)
	var sb strings.Builder
	for i, j := 0, 0; i < len(ra) || j < len(rb); {
		if i < len(ra) {
			sb.WriteRune(ra[i])
			i++
		}
		if j < len(rb) {
			sb.WriteRune(rb[j])
			j++
		}
	}
	return sb.String()
}

func md5hex(v string) string {
	sum := md5.Sum([]byte(v))
	return hex.EncodeToString(sum[:])
}

func bytesContainsFold(haystack, needle []byte) bool {
	return strings.Contains(strings.ToLower(string(haystack)), strings.ToLower(string(needle)))
}

func looksLikeConnectionDrop(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection aborted") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

func looksLikeTimeout(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		errors.Is(err, io.EOF)
}
