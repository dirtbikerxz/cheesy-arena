// TPLinkSwitch management and monitoring utilities.
// Provides login, reboot, and status polling for TL-SG108E v6 switches.

package network

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	loginPath        = "/logon.cgi"
	rebootPath       = "/reboot.cgi"
	rootPath         = "/"
	refererLoginPath = "/"
	refererRebtPath  = "/SystemRebootRpm.htm"

	switchPollPeriodSec = 1
)

type TPLinkSwitch struct {
	name         string
	address      string
	username     string
	password     string
	loginURL     string
	rebootURL    string
	rootURL      string
	refererLogin string
	refererRebt  string
	origin       string

	// Public status string: "UNKNOWN", "ERROR", "CONFIGURING", "ACTIVE".
	Status string

	// Reboot tracking for graceful "configuring" state.
	lastRebootAt time.Time
	rebootGrace  time.Duration
}

func NewTPLinkSwitch(name, address, username, password string) *TPLinkSwitch {
	return &TPLinkSwitch{
		name:         name,
		address:      address,
		username:     username,
		password:     password,
		loginURL:     "http://" + address + loginPath,
		rebootURL:    "http://" + address + rebootPath,
		rootURL:      "http://" + address + rootPath,
		refererLogin: "http://" + address + refererLoginPath,
		refererRebt:  "http://" + address + refererRebtPath,
		origin:       "http://" + address,

		Status:      "UNKNOWN",
		rebootGrace: 40 * time.Second,
	}
}

// Run starts a background loop to monitor the switch status every second.
func (TPLS *TPLinkSwitch) Run(isActive bool) {
	for {
		time.Sleep(time.Second * switchPollPeriodSec)
		if isActive {
			if err := TPLS.updateMonitoring(); err != nil {
				log.Printf("%s: Failed to update switch monitoring: %v", TPLS.name, err)
			}
		}
	}
}

// Reboot triggers a reboot via login + POST and sets Status to "configuring".
func (TPLS *TPLinkSwitch) Reboot() {
	if err := rebootWithLogin(TPLS); err != nil {
		log.Println(TPLS.name+": ERROR -", err)
	} else {
		TPLS.lastRebootAt = time.Now()
		log.Println(TPLS.name + ": Reboot request sent - " + TPLS.address)
	}
}

// updateMonitoring checks TCP port 80 reachability and updates Status.
func (TPLS *TPLinkSwitch) updateMonitoring() error {
	if TPLS.isReachableTCP80(500 * time.Millisecond) {
		if TPLS.Status != "ACTIVE" {
			if !TPLS.lastRebootAt.IsZero() && time.Since(TPLS.lastRebootAt) < TPLS.rebootGrace {
				log.Printf("%s: status changed from %s to ACTIVE in "+time.Since(TPLS.lastRebootAt).String()+" .", TPLS.name, TPLS.Status)
			} else {
				log.Printf("%s: status changed from %s to ACTIVE.", TPLS.name, TPLS.Status)
			}
		}
		TPLS.Status = "ACTIVE"
		return nil
	}

	// Within grace window → keep reporting "configuring".
	if !TPLS.lastRebootAt.IsZero() && time.Since(TPLS.lastRebootAt) < TPLS.rebootGrace {
		if TPLS.Status != "CONFIGURING" {
			log.Printf("%s: status changed from %s to CONFIGURING.", TPLS.name, TPLS.Status)
		}
		TPLS.Status = "CONFIGURING"
		return nil
	}

	// Outside grace window and unreachable → "error".
	if TPLS.Status != "ERROR" {
		log.Printf("%s: status changed from %s to ERROR.", TPLS.name, TPLS.Status)
	}
	TPLS.Status = "ERROR"
	return nil
}

// isReachableTCP80 does a quick TCP dial to port 80.
func (TPLS *TPLinkSwitch) isReachableTCP80(timeout time.Duration) bool {
	addr := TPLS.address + ":80"
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// --- Existing helpers ---

func rebootWithLogin(TPLS *TPLinkSwitch) error {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 2 * time.Second}

	if err := getRoot(client, TPLS); err != nil {
		return fmt.Errorf("prefetch root: %w", err)
	}
	if err := performLogin(client, TPLS); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if err := performReboot(client, TPLS); err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	return nil
}

func getRoot(client *http.Client, TPLS *TPLinkSwitch) error {
	req, _ := http.NewRequest("GET", TPLS.rootURL, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func performLogin(client *http.Client, TPLS *TPLinkSwitch) error {
	form := url.Values{}
	form.Set("username", TPLS.username)
	form.Set("password", TPLS.password)
	form.Set("cpassword", "")
	form.Set("logon", "Login")

	req, _ := http.NewRequest("POST", TPLS.loginURL, bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", TPLS.origin)
	req.Header.Set("Referer", TPLS.refererLogin)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && (resp.StatusCode < 300 || resp.StatusCode > 399) {
		return fmt.Errorf("unexpected login status: %s", resp.Status)
	}
	return nil
}

func performReboot(client *http.Client, TPLS *TPLinkSwitch) error {
	form := []byte("reboot_op=reboot&save_op=false")
	req, _ := http.NewRequest("POST", TPLS.rebootURL, bytes.NewBuffer(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", TPLS.origin)
	req.Header.Set("Referer", TPLS.refererRebt)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode < 300 || resp.StatusCode > 399 {
			return fmt.Errorf("reboot failed: %s", resp.Status)
		}
	}
	return nil
}
