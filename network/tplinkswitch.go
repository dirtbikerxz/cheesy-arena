package network

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	// 	username     = "admin"
	// 	password     = "password"
	// 	switchIP     = "10.0.100.10"
	// baseURL      = "http://" + switchIP
	// loginURL     = baseURL + "/logon.cgi"
	// rebootURL    = baseURL + "/reboot.cgi"
	// rootURL      = baseURL + "/"
	// refererLogin = baseURL + "/"
	// refererRebt  = baseURL + "/SystemRebootRpm.htm"
	// origin       = baseURL

	loginPath        = "/logon.cgi"
	rebootPath       = "/reboot.cgi"
	rootPath         = "/"
	refererLoginPath = "/"
	refererRebtPath  = "/SystemRebootRpm.htm"
)

type TPLinkSwitch struct {
	address      string
	username     string
	password     string
	loginURL     string
	rebootURL    string
	rootURL      string
	refererLogin string
	refererRebt  string
	origin       string
}

func NewTPLinkSwitch(address, username, password string) *TPLinkSwitch {
	return &TPLinkSwitch{
		address:      address,
		username:     username,
		password:     password,
		loginURL:     address + loginPath,
		rebootURL:    address + rebootPath,
		rootURL:      address + rootPath,
		refererLogin: address + refererLoginPath,
		refererRebt:  address + refererRebtPath,
		origin:       address,
	}
}

func RebootTPLinkSwitch(TPLS *TPLinkSwitch) {
	if err := rebootWithLogin(TPLS); err != nil {
		fmt.Println("ERROR:", err)
	} else {
		fmt.Println("Reboot request sent.")
	}
}

func rebootWithLogin(TPLS *TPLinkSwitch) error {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 2 * time.Second}

	// 0) GET / to receive initial H_P_SSID cookie
	if err := getRoot(client, TPLS); err != nil {
		return fmt.Errorf("prefetch root: %w", err)
	}

	// 1) LOGIN
	if err := performLogin(client, TPLS); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// 2) REBOOT
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
	// Some firmwares 302 here; cookie jar still captures Set-Cookie.
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

	// Many units return 200 or a redirect on success. Either is fine as long as the cookie jar holds H_P_SSID.
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
		// Some firmwares 302 to a “rebooting…” page; treat 2xx/3xx as success.
		if resp.StatusCode < 300 || resp.StatusCode > 399 {
			return fmt.Errorf("reboot failed: %s", resp.Status)
		}
	}
	return nil
}
