package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Team254/cheesy-arena/field"
	"golang.org/x/crypto/ssh"
)

// ===== Page model & job state =====

type rpiPageData struct {
	DisplayID  string
	Host       string
	ScanSubnet string
	StaticHost string
	SubnetMask string
	Gateway    string
	DNS        string
	SSHUser    string
	SSHPass    string

	State string
	Log   string
}

type rpiStopsPageData struct {
	StationId  string
	Host       string
	ScanSubnet string
	StaticHost string
	SubnetMask string
	Gateway    string
	DNS        string
	SSHUser    string
	SSHPass    string
	ApiHost    string
	Secret     string
	State      string
	Log        string
}

type rpiJob struct {
	mu    sync.Mutex
	state string
	log   []string
	last  rpiPageData
}

func (j *rpiJob) setState(s string) { j.mu.Lock(); j.state = s; j.mu.Unlock() }
func (j *rpiJob) appendf(f string, a ...any) {
	j.mu.Lock()
	j.log = append(j.log, fmt.Sprintf(f, a...))
	j.mu.Unlock()
}
func (j *rpiJob) snapshot() (string, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state, strings.Join(j.log, "\n")
}
func (j *rpiJob) getLast() rpiPageData  { j.mu.Lock(); defer j.mu.Unlock(); return j.last }
func (j *rpiJob) setLast(p rpiPageData) { j.mu.Lock(); j.last = p; j.mu.Unlock() }

type rpiStopsJob struct {
	mu    sync.Mutex
	state string
	log   []string
	last  rpiStopsPageData
}

func (j *rpiStopsJob) setState(s string) { j.mu.Lock(); j.state = s; j.mu.Unlock() }
func (j *rpiStopsJob) appendf(f string, a ...any) {
	j.mu.Lock()
	j.log = append(j.log, fmt.Sprintf(f, a...))
	j.mu.Unlock()
}
func (j *rpiStopsJob) snapshot() (string, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state, strings.Join(j.log, "\n")
}
func (j *rpiStopsJob) getLast() rpiStopsPageData  { j.mu.Lock(); defer j.mu.Unlock(); return j.last }
func (j *rpiStopsJob) setLast(p rpiStopsPageData) { j.mu.Lock(); j.last = p; j.mu.Unlock() }

var rpiGlobal rpiJob
var rpiStopsGlobal rpiStopsJob

const defaultStationRpiApiHost = "http://10.0.100.5:8080"

// ===== Handlers =====

func (web *Web) rpiGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	last := rpiGlobal.getLast()
	// Provide sticky-friendly defaults for first visit only
	if last.DisplayID == "" {
		last.DisplayID = "FTA1"
	}
	if last.ScanSubnet == "" {
		last.ScanSubnet = "10.0.100.0/24"
	}
	if last.SSHUser == "" {
		last.SSHUser = "admin"
	}
	if last.SSHPass == "" {
		last.SSHPass = "1234Five"
	}

	if last.Host == "" {
		last.Host = "10.0.100.199"
	}
	if last.SubnetMask == "" {
		last.SubnetMask = "255.255.255.0"
	}
	if last.Gateway == "" {
		last.Gateway = "10.0.100.2"
	}
	if last.DNS == "" {
		last.DNS = "10.0.100.2"
	}

	state, log := rpiGlobal.snapshot()
	last.State = state
	last.Log = log

	tpl, err := web.parseFiles("templates/setup_rpi.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := map[string]any{
		"EventSettings": web.safeEventSettings(), // base.html expects Name/NetworkSecurityEnabled
		"Rpi":           last,
	}
	if err = tpl.ExecuteTemplate(w, "base", data); err != nil {
		handleWebErr(w, err)
		return
	}
}

func (web *Web) rpiRunPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		handleWebErr(w, err)
		return
	}
	form := rpiPageData{
		DisplayID:  strings.TrimSpace(r.FormValue("displayId")),
		Host:       strings.TrimSpace(r.FormValue("host")),
		ScanSubnet: strings.TrimSpace(r.FormValue("scanSubnet")),
		StaticHost: strings.TrimSpace(r.FormValue("staticHost")),
		SubnetMask: strings.TrimSpace(r.FormValue("subnetMask")),
		Gateway:    strings.TrimSpace(r.FormValue("gateway")),
		DNS:        strings.TrimSpace(r.FormValue("dns")),
		SSHUser:    strings.TrimSpace(r.FormValue("sshUser")),
		SSHPass:    strings.TrimSpace(r.FormValue("sshPass")),
	}
	// Defaults where appropriate
	if form.DisplayID == "" {
		form.DisplayID = "FTA1"
	}
	if form.ScanSubnet == "" {
		form.ScanSubnet = "10.0.100.0/24"
	}
	if form.SSHUser == "" {
		form.SSHUser = "admin"
	}
	if form.SSHPass == "" {
		form.SSHPass = "1234Five"
	}

	if form.Host == "" {
		form.Host = "10.0.100.199"
	}
	if form.SubnetMask == "" {
		form.SubnetMask = "255.255.255.0"
	}
	if form.Gateway == "" {
		form.Gateway = "10.0.100.2"
	}
	if form.DNS == "" {
		form.DNS = "10.0.100.2"
	}

	// Required IP inputs
	if form.StaticHost == "" || net.ParseIP(form.StaticHost) == nil {
		http.Error(w, "Static IP (host) required and must be IPv4, e.g. 10.0.100.41", http.StatusBadRequest)
		return
	}
	if form.SubnetMask == "" {
		http.Error(w, "Subnet mask required (e.g., 255.255.255.0 or 24)", http.StatusBadRequest)
		return
	}
	cidrLen, err := parseMaskToCIDR(form.SubnetMask)
	if err != nil {
		http.Error(w, "Invalid subnet mask: "+err.Error(), http.StatusBadRequest)
		return
	}
	if form.Gateway == "" || net.ParseIP(form.Gateway) == nil {
		http.Error(w, "Gateway required and must be IPv4, e.g. 10.0.100.2", http.StatusBadRequest)
		return
	}
	if form.DNS == "" || net.ParseIP(firstIP(form.DNS)) == nil {
		http.Error(w, "DNS required and must be IPv4, e.g. 10.0.100.2", http.StatusBadRequest)
		return
	}
	// Persist sticky values
	rpiGlobal.setLast(form)

	url := fmt.Sprintf("http://10.0.100.5:8080/display?displayId=%s", form.DisplayID)
	staticCIDR := fmt.Sprintf("%s/%d", form.StaticHost, cidrLen)
	dnsFirst := firstIP(form.DNS)

	// Only one run at a time
	rpiGlobal.mu.Lock()
	if rpiGlobal.state == "Running" {
		rpiGlobal.mu.Unlock()
		http.Error(w, "Provisioning already running", http.StatusConflict)
		return
	}
	rpiGlobal.state = "Running"
	rpiGlobal.log = []string{fmt.Sprintf(
		"Starting... URL=%s host=%s scanSubnet=%s static=%s gw=%s dns=%s user=%s",
		url, form.Host, form.ScanSubnet, staticCIDR, form.Gateway, dnsFirst, form.SSHUser,
	)}
	rpiGlobal.mu.Unlock()

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				rpiGlobal.appendf("panic: %v", rec)
				rpiGlobal.setState("Error")
			}
		}()

		var targets []string
		if form.Host != "" {
			targets = []string{form.Host}
		} else {
			rpiGlobal.appendf("Scanning %s for SSH...", form.ScanSubnet)
			found, err := scanSubnetForSSH(form.ScanSubnet)
			if err != nil {
				rpiGlobal.appendf("Scan error: %v", err)
			}
			if len(found) == 0 {
				rpiGlobal.appendf("No hosts found.")
				rpiGlobal.setState("Error")
				return
			}
			// Optional: de-prioritize likely gateway IPs (.1, .254) by moving them to the end.
			var prios, deprios []string
			for _, ip := range found {
				if strings.HasSuffix(ip, ".1") || strings.HasSuffix(ip, ".254") {
					deprios = append(deprios, ip)
				} else {
					prios = append(prios, ip)
				}
			}
			targets = append(prios, deprios...)
			rpiGlobal.appendf("Found: %s", strings.Join(found, ", "))
		}

		var successes []string
		var failures []string

		for _, t := range targets {
			rpiGlobal.appendf("Connecting: %s", t)
			if err := configurePi(t, form.SSHUser, form.SSHPass, url, staticCIDR, form.Gateway, dnsFirst, &rpiGlobal); err != nil {
				failures = append(failures, fmt.Sprintf("%s (%v)", t, err))
				rpiGlobal.appendf("Error configuring %s: %v", t, err)
				// keep going; try the next host
				continue
			}
			successes = append(successes, t)
		}

		if len(successes) > 0 {
			rpiGlobal.appendf("Configured OK: %s", strings.Join(successes, ", "))
			if len(failures) > 0 {
				rpiGlobal.appendf("Failures: %s", strings.Join(failures, "; "))
			}
			rpiGlobal.setState("Done")
		} else {
			if len(failures) > 0 {
				rpiGlobal.appendf("All attempts failed: %s", strings.Join(failures, "; "))
			} else {
				rpiGlobal.appendf("No targets attempted.")
			}
			rpiGlobal.setState("Error")
		}
	}()

	http.Redirect(w, r, "/setup/rpi", http.StatusSeeOther)
}

func (web *Web) rpiStatusHandler(w http.ResponseWriter, r *http.Request) {
	state, log := rpiGlobal.snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"state": state,
		"log":   log,
	})
}

func (web *Web) rpiStopsGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	last := rpiStopsGlobal.getLast()
	if last.StationId == "" {
		last.StationId = "R1"
	}
	if last.ScanSubnet == "" {
		last.ScanSubnet = "10.0.100.0/24"
	}
	if last.SSHUser == "" {
		last.SSHUser = "admin"
	}
	if last.SSHPass == "" {
		last.SSHPass = "1234Five"
	}
	if last.ApiHost == "" {
		last.ApiHost = defaultStationRpiApiHost
	}
	if last.Host == "" {
		last.Host = "10.0.100.199"
	}
	if last.SubnetMask == "" {
		last.SubnetMask = "255.255.255.0"
	}
	if last.Gateway == "" {
		last.Gateway = "10.0.100.2"
	}
	if last.DNS == "" {
		last.DNS = "10.0.100.2"
	}
	if last.Secret == "" && web.arena.EventSettings != nil {
		last.Secret = web.arena.EventSettings.StationRpiSecret
	}
	state, log := rpiStopsGlobal.snapshot()
	last.State = state
	last.Log = log

	tpl, err := web.parseFiles("templates/setup_rpi_stops.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := map[string]any{
		"EventSettings": web.safeEventSettings(),
		"RpiStops":      last,
		"StationStatus": web.buildStationRpiStatusView(),
	}
	if err = tpl.ExecuteTemplate(w, "base", data); err != nil {
		handleWebErr(w, err)
		return
	}
}

func (web *Web) rpiStopsRunPostHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		handleWebErr(w, err)
		return
	}
	form := rpiStopsPageData{
		StationId:  strings.TrimSpace(r.FormValue("stationId")),
		Host:       strings.TrimSpace(r.FormValue("host")),
		ScanSubnet: strings.TrimSpace(r.FormValue("scanSubnet")),
		StaticHost: strings.TrimSpace(r.FormValue("staticHost")),
		SubnetMask: strings.TrimSpace(r.FormValue("subnetMask")),
		Gateway:    strings.TrimSpace(r.FormValue("gateway")),
		DNS:        strings.TrimSpace(r.FormValue("dns")),
		SSHUser:    strings.TrimSpace(r.FormValue("sshUser")),
		SSHPass:    strings.TrimSpace(r.FormValue("sshPass")),
		ApiHost:    strings.TrimSpace(r.FormValue("apiHost")),
		Secret:     strings.TrimSpace(r.FormValue("secret")),
	}
	if form.StationId == "" {
		form.StationId = "R1"
	}
	if form.ScanSubnet == "" {
		form.ScanSubnet = "10.0.100.0/24"
	}
	if form.SSHUser == "" {
		form.SSHUser = "admin"
	}
	if form.SSHPass == "" {
		form.SSHPass = "1234Five"
	}
	if form.ApiHost == "" {
		form.ApiHost = defaultStationRpiApiHost
	}
	form.ApiHost = strings.TrimSuffix(form.ApiHost, "/")
	if form.Host == "" {
		form.Host = "10.0.100.199"
	}
	if form.SubnetMask == "" {
		form.SubnetMask = "255.255.255.0"
	}
	if form.Gateway == "" {
		form.Gateway = "10.0.100.2"
	}
	if form.DNS == "" {
		form.DNS = "10.0.100.2"
	}
	if form.Secret == "" && web.arena.EventSettings != nil {
		form.Secret = web.arena.EventSettings.StationRpiSecret
	}

	form.StationId = strings.ToUpper(form.StationId)
	if !isValidStationId(form.StationId) {
		http.Error(w, fmt.Sprintf("Invalid station ID '%s'.", form.StationId), http.StatusBadRequest)
		return
	}
	if form.StaticHost == "" || net.ParseIP(form.StaticHost) == nil {
		http.Error(w, "Static IP (host) required and must be IPv4, e.g. 10.0.100.41", http.StatusBadRequest)
		return
	}
	if form.SubnetMask == "" {
		http.Error(w, "Subnet mask required (e.g., 255.255.255.0 or 24)", http.StatusBadRequest)
		return
	}
	cidrLen, err := parseMaskToCIDR(form.SubnetMask)
	if err != nil {
		http.Error(w, "Invalid subnet mask: "+err.Error(), http.StatusBadRequest)
		return
	}
	if form.Gateway == "" || net.ParseIP(form.Gateway) == nil {
		http.Error(w, "Gateway required and must be IPv4, e.g. 10.0.100.2", http.StatusBadRequest)
		return
	}
	if form.DNS == "" || net.ParseIP(firstIP(form.DNS)) == nil {
		http.Error(w, "DNS required and must be IPv4, e.g. 10.0.100.2", http.StatusBadRequest)
		return
	}

	rpiStopsGlobal.setLast(form)

	staticCIDR := fmt.Sprintf("%s/%d", form.StaticHost, cidrLen)
	apiHost := form.ApiHost
	apiURL := fmt.Sprintf("%s/api/stations/%s/stops", apiHost, form.StationId)

	rpiStopsGlobal.mu.Lock()
	if rpiStopsGlobal.state == "Running" {
		rpiStopsGlobal.mu.Unlock()
		http.Error(w, "Provisioning already running", http.StatusConflict)
		return
	}
	rpiStopsGlobal.state = "Running"
	rpiStopsGlobal.log = []string{fmt.Sprintf(
		"Starting... station=%s host=%s scanSubnet=%s static=%s gw=%s dns=%s api=%s",
		form.StationId, form.Host, form.ScanSubnet, staticCIDR, form.Gateway, firstIP(form.DNS), apiHost,
	)}
	rpiStopsGlobal.mu.Unlock()

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				rpiStopsGlobal.appendf("panic: %v", rec)
				rpiStopsGlobal.setState("Error")
			}
		}()

		var targets []string
		if form.Host != "" {
			targets = []string{form.Host}
		} else {
			rpiStopsGlobal.appendf("Scanning %s for SSH...", form.ScanSubnet)
			found, err := scanSubnetForSSH(form.ScanSubnet)
			if err != nil {
				rpiStopsGlobal.appendf("Scan error: %v", err)
			}
			if len(found) == 0 {
				rpiStopsGlobal.appendf("No hosts found.")
				rpiStopsGlobal.setState("Error")
				return
			}
			var prios, deprios []string
			for _, ip := range found {
				if strings.HasSuffix(ip, ".1") || strings.HasSuffix(ip, ".254") {
					deprios = append(deprios, ip)
				} else {
					prios = append(prios, ip)
				}
			}
			targets = append(prios, deprios...)
			rpiStopsGlobal.appendf("Found: %s", strings.Join(found, ", "))
		}

		var successes []string
		var failures []string

		for _, t := range targets {
			rpiStopsGlobal.appendf("Connecting: %s", t)
			if err := configureStopsPi(
				t, form.SSHUser, form.SSHPass, apiURL, staticCIDR, form.Gateway, form.DNS,
				form.StationId, form.Secret, &rpiStopsGlobal,
			); err != nil {
				failures = append(failures, fmt.Sprintf("%s (%v)", t, err))
				rpiStopsGlobal.appendf("Error configuring %s: %v", t, err)
				continue
			}
			successes = append(successes, t)
		}

		if len(successes) > 0 {
			rpiStopsGlobal.appendf("Configured OK: %s", strings.Join(successes, ", "))
			if len(failures) > 0 {
				rpiStopsGlobal.appendf("Failures: %s", strings.Join(failures, "; "))
			}
			rpiStopsGlobal.setState("Done")
		} else {
			if len(failures) > 0 {
				rpiStopsGlobal.appendf("All attempts failed: %s", strings.Join(failures, "; "))
			} else {
				rpiStopsGlobal.appendf("No targets attempted.")
			}
			rpiStopsGlobal.setState("Error")
		}
	}()

	http.Redirect(w, r, "/setup/rpi/stops", http.StatusSeeOther)
}

func (web *Web) rpiStopsStatusHandler(w http.ResponseWriter, r *http.Request) {
	state, log := rpiStopsGlobal.snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"state":         state,
		"log":           log,
		"stationStatus": web.buildStationRpiStatusView(),
	})
}

type stationRpiStatusView struct {
	Station     string
	Online      bool
	RemoteEStop bool
	RemoteAStop bool
	LastUpdate  string
}

func (web *Web) buildStationRpiStatusView() []stationRpiStatusView {
	statusMap := web.arena.StationRpiStatuses()
	order := []string{"R1", "R2", "R3", "B1", "B2", "B3"}
	result := make([]stationRpiStatusView, 0, len(order))
	for _, station := range order {
		status, ok := statusMap[station]
		if !ok {
			status = field.StationRpiStatus{}
		}
		lastUpdate := "Never"
		if !status.LastUpdate.IsZero() {
			lastUpdate = status.LastUpdate.Format("15:04:05")
		}
		result = append(result, stationRpiStatusView{
			Station:     station,
			Online:      status.Online,
			RemoteEStop: status.RemoteEStop,
			RemoteAStop: status.RemoteAStop,
			LastUpdate:  lastUpdate,
		})
	}
	return result
}

func isValidStationId(station string) bool {
	switch station {
	case "R1", "R2", "R3", "B1", "B2", "B3":
		return true
	default:
		return false
	}
}

// ===== IP helpers =====

func parseMaskToCIDR(mask string) (int, error) {
	mask = strings.TrimSpace(mask)
	if mask == "" {
		return 0, fmt.Errorf("empty")
	}
	if n, err := strconv.Atoi(mask); err == nil {
		if n < 0 || n > 32 {
			return 0, fmt.Errorf("CIDR out of range")
		}
		return n, nil
	}
	ip := net.ParseIP(mask)
	if ip == nil {
		return 0, fmt.Errorf("not a valid IPv4 dotted mask")
	}
	ip = ip.To4()
	if ip == nil {
		return 0, fmt.Errorf("mask is not IPv4")
	}
	ones := 0
	for _, b := range []byte(ip) {
		for i := 7; i >= 0; i-- {
			if (b>>uint(i))&1 == 1 {
				ones++
			} else {
				if (b & ((1 << uint(i)) - 1)) != 0 {
					return 0, fmt.Errorf("non-contiguous netmask")
				}
				break
			}
		}
	}
	return ones, nil
}

func firstIP(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	for _, f := range fields {
		ip := net.ParseIP(strings.TrimSpace(f))
		if ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}

// ===== Net scan (SSH) =====

func scanSubnetForSSH(cidr string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("bad CIDR: %w", err)
	}
	var found []string
	var ips []net.IP
	for ip := ipNet.IP.Mask(ipNet.Mask); ipNet.Contains(ip); ip = incIP(ip) {
		cp := make(net.IP, len(ip))
		copy(cp, ip)
		ips = append(ips, cp)
	}
	if len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	} else {
		ips = []net.IP{}
	}

	sem := make(chan struct{}, 128)
	type result struct {
		ip string
		ok bool
	}
	out := make(chan result, len(ips))
	for _, ip := range ips {
		ip := ip
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			addr := net.JoinHostPort(ip.String(), "22")
			c, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err == nil {
				_ = c.Close()
				out <- result{ip: ip.String(), ok: true}
			} else {
				out <- result{ok: false}
			}
		}()
	}
	for i := 0; i < len(ips); i++ {
		r := <-out
		if r.ok {
			found = append(found, r.ip)
		}
	}
	return found, nil
}

func incIP(ip net.IP) net.IP {
	res := make(net.IP, len(ip))
	copy(res, ip)
	for j := len(res) - 1; j >= 0; j-- {
		res[j]++
		if res[j] > 0 {
			break
		}
	}
	return res
}

// ===== SSH + remote script bootstrap =====

// configurePi uploads the rendered script and launches it as *root* on the remote Pi,
// sending all output to /var/log/rpi-setup.log so you can tail it.
func configurePi(host, user, pass, url, staticCIDR, gw, dns string, job *rpiJob) error {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         4 * time.Second,
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), cfg)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer conn.Close()

	// Render the script
	script, err := renderRpiScript(RpiParams{
		User:       user,
		Url:        url,
		StaticCidr: staticCIDR,
		Gateway:    gw,
		Dns:        dns,
	})
	if err != nil {
		return fmt.Errorf("render script: %w", err)
	}

	out, err := runScriptDetachedAsRoot(conn, script, "/var/log/rpi-setup.log", pass)
	job.appendf("%s", strings.TrimSpace(out))
	return err
}

// runScriptDetachedAsRoot writes the script to /tmp and starts it with sudo as root,
// detaching via setsid+nohup. We pass the sudo password via -S to ensure elevation.
func runScriptDetachedAsRoot(conn *ssh.Client, script, remoteLog, sudoPassword string) (string, error) {
	sess, err := conn.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	// 1) Upload to /tmp
	upload := `bash -lc '
TMP=/tmp/rpi-role.sh
cat > "$TMP" <<'"'EOF_SCRIPT'"'
` + escapeSingleQuotes(script) + `
EOF_SCRIPT
# 2) Convert CRLF -> LF (Windows builds) and make it executable
sed -i "s/\r$//" "$TMP"
chmod +x "$TMP"
echo "REMOTE: script uploaded to $TMP"
'`

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf
	if err := sess.Run(upload); err != nil {
		return outBuf.String() + errBuf.String(), fmt.Errorf("upload script: %w", err)
	}

	// 3) Launch as root, detached, logging to /var/log/rpi-setup.log
	sess2, err := conn.NewSession()
	if err != nil {
		return "", err
	}
	defer sess2.Close()

	// Use sudo -S (read password from stdin), disable prompt text with -p "".
	launch := `sudo -S -p "" bash -lc 'setsid nohup /bin/bash /tmp/rpi-role.sh > ` + remoteLog + ` 2>&1 & echo "REMOTE: started (/tmp/rpi-role.sh); log: ` + remoteLog + `"'`

	var outBuf2, errBuf2 bytes.Buffer
	sess2.Stdout = &outBuf2
	sess2.Stderr = &errBuf2

	stdin, _ := sess2.StdinPipe()
	go func() {
		defer stdin.Close()
		io.WriteString(stdin, sudoPassword+"\n")
	}()

	if err := sess2.Run(launch); err != nil {
		return outBuf2.String() + errBuf2.String(), fmt.Errorf("sudo launch: %w", err)
	}
	return outBuf2.String(), nil
}

func configureStopsPi(host, user, pass, apiURL, staticCIDR, gw, dns, stationId, secret string, job *rpiStopsJob) error {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         4 * time.Second,
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), cfg)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer conn.Close()

	script, err := renderRpiStopsScript(RpiStopsParams{
		User:      user,
		ApiUrl:    apiURL,
		StationId: stationId,
		Secret:    secret,
		StaticCidr: staticCIDR,
		Gateway:   gw,
		Dns:       dns,
	})
	if err != nil {
		return fmt.Errorf("render script: %w", err)
	}

	out, err := runScriptDetachedAsRoot(conn, script, "/var/log/rpi-stops-setup.log", pass)
	job.appendf("%s", strings.TrimSpace(out))
	return err
}

func escapeSingleQuotes(s string) string { return strings.ReplaceAll(s, `'`, `'\''`) }

// ===== Embedded assets =====

//go:embed assets/rpi/*
var rpiAssets embed.FS

type RpiParams struct {
	User       string
	Url        string
	StaticCidr string
	Gateway    string
	Dns        string
}

type RpiStopsParams struct {
	User       string
	ApiUrl     string
	StationId  string
	Secret     string
	StaticCidr string
	Gateway    string
	Dns        string
}

func renderRpiScript(p RpiParams) (string, error) {
	return renderRpiTemplate("assets/rpi/display.sh.tmpl", p)
}

func renderRpiStopsScript(p RpiStopsParams) (string, error) {
	return renderRpiTemplate("assets/rpi/stops.sh.tmpl", p)
}

func renderRpiTemplate(filename string, data any) (string, error) {
	commonBytes, err := rpiAssets.ReadFile("assets/rpi/common.sh.inc")
	if err != nil {
		return "", err
	}
	bodyBytes, err := rpiAssets.ReadFile(filename)
	if err != nil {
		return "", err
	}

	combined := string(commonBytes) + "\n\n" + string(bodyBytes)

	t := template.New("rpi-script")
	t, err = t.Parse(combined)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ===== base.html compatibility =====

func (web *Web) safeEventSettings() any {
	// base.html references .EventSettings.Name and .EventSettings.NetworkSecurityEnabled
	type minimal struct {
		Name                   string
		NetworkSecurityEnabled bool
	}
	return minimal{Name: "", NetworkSecurityEnabled: false}
}
