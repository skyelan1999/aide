package server

// Generic, opt-in collection only. This bridge never invokes tools or writes project files.
import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type captureItem struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Unit  string   `json:"unit"`
	Codes []string `json:"codes"`
}
type captureRecord struct {
	ID       string  `json:"id"`
	Item     string  `json:"item"`
	Zone     string  `json:"zone"`
	Device   string  `json:"device"`
	Quantity float64 `json:"quantity"`
	Version  int     `json:"version"`
	At       string  `json:"at"`
}
type captureDevice struct {
	ID, Name, Zone, Code    string
	Secret                  [32]byte
	Desktop, Phone, Revoked bool
	Seen                    time.Time
}
type captureInvite struct {
	Secret  [32]byte
	Expires time.Time
	Used    bool
}
type mobileCapture struct {
	ID, Title, Mode, Baseline, Origin string
	Expires                           time.Time
	Sealed, Stopped                   bool
	Items                             []captureItem
	Zones                             []string
	Devices                           map[string]*captureDevice
	Invites                           map[string]*captureInvite
	Records                           []captureRecord
	RateStart                         time.Time
	RateRequests                      int
	Seen                              map[string][32]byte
}

func captureSecret() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func captureHash(s string) [32]byte { return sha256.Sum256([]byte(s)) }
func captureEqual(secret string, want [32]byte) bool {
	h := captureHash(secret)
	return subtle.ConstantTimeCompare(h[:], want[:]) == 1
}
func captureString(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func captureClean(s string, max int) bool {
	return strings.TrimSpace(s) == s && s != "" && len(s) <= max && !strings.ContainsAny(s, "\x00\r\n")
}
func captureOwnerAllowed(a *App, b *pluginWorkbenchBridge) bool {
	for key, owner := range a.pluginWorkbenchBridges {
		if owner == b {
			_, e := a.workbenchBridgeLocked(b.Ticket.ID, key, false)
			return e == nil
		}
	}
	return false
}
func (a *App) captureEnabledLocked(id string) bool {
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Enabled && p.Workbench != nil {
			return p.Workbench.MobileCapture
		}
	}
	return false
}
func captureSnapshot(c *mobileCapture, phone *captureDevice) map[string]any {
	devices := []map[string]any{}
	for _, d := range c.Devices {
		if phone == nil {
			devices = append(devices, map[string]any{"id": d.ID, "name": d.Name, "zone": d.Zone, "code": d.Code, "desktop": d.Desktop, "phone": d.Phone, "revoked": d.Revoked, "lastSeen": d.Seen})
		}
	}
	rows := []captureRecord{}
	for _, r := range c.Records {
		if phone == nil || r.Zone == phone.Zone {
			rows = append(rows, r)
		}
	}
	out := map[string]any{"id": c.ID, "title": c.Title, "mode": c.Mode, "expires": c.Expires, "sealed": c.Sealed, "baseline": c.Baseline, "devices": devices, "records": rows, "zones": c.Zones}
	if phone == nil || (phone.Desktop && phone.Phone) {
		out["items"] = c.Items
	}
	if phone != nil {
		delete(out, "baseline")
		delete(out, "zones")
		delete(out, "devices")
		out["device"] = map[string]any{"id": phone.ID, "name": phone.Name, "zone": phone.Zone, "code": phone.Code, "desktop": phone.Desktop, "phone": phone.Phone}
		if !phone.Desktop || !phone.Phone {
			delete(out, "records")
			delete(out, "baseline")
			delete(out, "zones")
		}
	}
	return out
}
func captureOrigin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("需要纯 HTTPS 访问地址，不含路径、查询或凭据")
	}
	return "https://" + u.Host, nil
}
func (a *App) captureCertificate(origin string) error {
	u, _ := url.Parse(origin)
	raw, e := os.ReadFile(filepath.Join(CertsDir(a.dataPath), "cert.pem"))
	if e != nil {
		return errors.New("HTTPS 证书尚未配置")
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return errors.New("证书无效")
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil || cert.VerifyHostname(u.Hostname()) != nil || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
		return errors.New("现有 HTTPS 证书不覆盖该手机访问地址，请配置匹配的受信任证书；不能关闭证书校验")
	}
	return nil
}

// Caller holds a.mu and has verified the workbench bridge and opt-in manifest.
func (a *App) captureControlLocked(b *pluginWorkbenchBridge, args map[string]any) (any, error) {
	action := captureString(args, "action")
	now := time.Now()
	if action == "create" {
		if b.Captures == nil {
			b.Captures = map[string]*mobileCapture{}
		}
		for id, c := range b.Captures {
			if now.After(c.Expires) || c.Stopped {
				delete(b.Captures, id)
			}
		}
		if len(b.Captures) >= 4 {
			return nil, errors.New("每个工作台最多四张采集单，请结束旧单")
		}
		data, e := json.Marshal(args)
		if e != nil || len(data) > 120000 {
			return nil, errors.New("采集目录过大，请分批")
		}
		var in struct {
			Title    string        `json:"title"`
			Mode     string        `json:"mode"`
			Baseline string        `json:"baseline"`
			BaseURL  string        `json:"baseURL"`
			Items    []captureItem `json:"items"`
			Zones    []string      `json:"zones"`
		}
		if json.Unmarshal(data, &in) != nil {
			return nil, errors.New("采集配置无效")
		}
		if !captureClean(in.Title, 160) || !captureClean(in.Baseline, 128) || (in.Mode != "receipt" && in.Mode != "count") || len(in.Items) < 1 || len(in.Items) > 500 || len(in.Zones) < 1 || len(in.Zones) > 16 || len(in.Items)*len(in.Zones) > 500 {
			return nil, errors.New("采集单支持1至500种物料、1至16个区域，物料与区域组合最多500项")
		}
		origin, e := captureOrigin(in.BaseURL)
		if e != nil {
			return nil, e
		}
		if e = a.captureCertificate(origin); e != nil {
			return nil, e
		}
		codes := map[string]bool{}
		ids := map[string]bool{}
		zones := map[string]bool{}
		for _, z := range in.Zones {
			if !captureClean(z, 80) || zones[z] {
				return nil, errors.New("区域名称无效或重复")
			}
			zones[z] = true
		}
		for _, it := range in.Items {
			if !captureClean(it.ID, 64) || !captureClean(it.Name, 160) || len(it.Unit) > 32 || ids[it.ID] || len(it.Codes) < 1 || len(it.Codes) > 8 {
				return nil, errors.New("物料目录无效")
			}
			ids[it.ID] = true
			for _, code := range it.Codes {
				if !captureClean(code, 128) || codes[code] {
					return nil, errors.New("条码为空、重复或过长")
				}
				codes[code] = true
			}
		}
		c := &mobileCapture{ID: captureSecret()[:32], Title: in.Title, Mode: in.Mode, Baseline: in.Baseline, Origin: origin, Expires: now.Add(2 * time.Hour), Items: in.Items, Zones: in.Zones, Devices: map[string]*captureDevice{}, Invites: map[string]*captureInvite{}, Seen: map[string][32]byte{}}
		b.Captures[c.ID] = c
		return captureSnapshot(c, nil), nil
	}
	c := b.Captures[captureString(args, "id")]
	if c == nil || now.After(c.Expires) {
		return nil, errors.New("采集单已失效")
	}
	switch action {
	case "status":
		return captureSnapshot(c, nil), nil
	case "invite":
		if c.Sealed {
			return nil, errors.New("采集单已封存")
		}
		for id, v := range c.Invites {
			if now.After(v.Expires) {
				delete(c.Invites, id)
			}
		}
		if len(c.Invites) >= 16 || len(c.Devices) >= 16 {
			return nil, errors.New("最多16台设备或待用邀请")
		}
		id, secret := captureSecret()[:32], captureSecret()
		c.Invites[id] = &captureInvite{Secret: captureHash(secret), Expires: now.Add(2 * time.Minute)}
		return map[string]any{"url": c.Origin + "/mobile-capture.html#session=" + c.ID + "&invite=" + id + "&secret=" + secret, "expires": c.Invites[id].Expires}, nil
	case "approve":
		d := c.Devices[captureString(args, "device")]
		zone := captureString(args, "zone")
		if d == nil || d.Revoked || c.Sealed {
			return nil, errors.New("设备或采集单不可授权")
		}
		ok := false
		for _, z := range c.Zones {
			ok = ok || z == zone
		}
		if d.Desktop && d.Zone != zone {
			return nil, errors.New("已授权设备不能更换区域，请撤销后重新配对")
		}
		if !ok {
			return nil, errors.New("区域无效")
		}
		for _, other := range c.Devices {
			if other != d && !other.Revoked && other.Desktop && other.Zone == zone {
				return nil, errors.New("该区域已分配给其他设备，请先撤销")
			}
		}
		d.Zone = zone
		d.Desktop = true
	case "revoke":
		d := c.Devices[captureString(args, "device")]
		if d == nil {
			return nil, errors.New("设备不存在")
		}
		d.Revoked = true
	case "seal":
		c.Sealed = true
	case "stop":
		c.Sealed = true
		c.Stopped = true
		for _, d := range c.Devices {
			d.Revoked = true
		}
	default:
		return nil, errors.New("不支持的采集操作")
	}
	return captureSnapshot(c, nil), nil
}
func (a *App) mobileCapturePhone(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.TLS == nil || r.Header.Get("Origin") != "https://"+r.Host || r.Header.Get("Content-Type") != "application/json" {
		fail(w, 403, errors.New("需要同源 HTTPS JSON 请求"))
		return
	}
	var in struct {
		Session  string   `json:"session"`
		Action   string   `json:"action"`
		Invite   string   `json:"invite"`
		Secret   string   `json:"secret"`
		Name     string   `json:"name"`
		Event    string   `json:"event"`
		Code     string   `json:"code"`
		Quantity *float64 `json:"quantity"`
		Version  int      `json:"version"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&in); e != nil {
		fail(w, 400, errors.New("请求无效"))
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, errors.New("请求包含多余内容"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var c *mobileCapture
	var owner *pluginWorkbenchBridge
	for _, b := range a.pluginWorkbenchBridges {
		if found := b.Captures[in.Session]; found != nil {
			c, owner = found, b
			break
		}
	}
	if c == nil || time.Now().After(c.Expires) || c.Origin != "https://"+r.Host || !captureOwnerAllowed(a, owner) || !a.captureEnabledLocked(owner.Ticket.ID) {
		fail(w, 403, errors.New("采集授权已失效"))
		return
	}
	now := time.Now()
	// A bounded per-session window, with no attacker-controlled address map.
	if now.Sub(c.RateStart) >= time.Minute {
		c.RateStart = now
		c.RateRequests = 0
	}
	c.RateRequests++
	if c.RateRequests > 600 {
		fail(w, 429, errors.New("采集请求过快，请稍后重试"))
		return
	}
	if in.Action == "join" {
		invite := c.Invites[in.Invite]
		if c.Sealed || invite == nil || invite.Used || now.After(invite.Expires) || !captureEqual(in.Secret, invite.Secret) || !captureClean(in.Name, 80) || len(c.Devices) >= 16 {
			fail(w, 403, errors.New("邀请无效、已用或过期"))
			return
		}
		invite.Used = true
		secret := captureSecret()
		id := captureSecret()[:32]
		h := captureHash(secret)
		code := fmt.Sprintf("%06d", uint32(h[0])<<16|uint32(h[1])<<8|uint32(h[2]))
		code = code[len(code)-6:]
		d := &captureDevice{ID: id, Name: in.Name, Code: code, Secret: h, Seen: now}
		c.Devices[id] = d
		jsonOut(w, 200, map[string]any{"token": id + "." + secret, "state": captureSnapshot(c, d)})
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Capture ")
	parts := strings.Split(token, ".")
	var d *captureDevice
	if len(parts) == 2 {
		d = c.Devices[parts[0]]
	}
	if d == nil || d.Revoked || now.Sub(d.Seen) > 10*time.Minute || !captureEqual(parts[1], d.Secret) {
		fail(w, 403, errors.New("设备授权已撤销或过期"))
		return
	}
	d.Seen = now
	switch in.Action {
	case "confirm":
		if c.Sealed {
			fail(w, 409, errors.New("采集已封存"))
			return
		}
		d.Phone = true
	case "status":
	case "record":
		if in.Quantity == nil {
			fail(w, 400, errors.New("必须明确填写数量，零库存也需提交零"))
			return
		}
		quantity := *in.Quantity
		if c.Sealed || !d.Phone || !d.Desktop {
			fail(w, 403, errors.New("需要双方确认，且采集单未封存"))
			return
		}
		if !captureClean(in.Event, 80) || !captureClean(in.Code, 128) || math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity < 0 || quantity > 1e9 || math.Abs(quantity*1e6-math.Round(quantity*1e6)) > 0.0001 || (c.Mode == "receipt" && quantity == 0) {
			fail(w, 400, errors.New("条码、事件或数量无效"))
			return
		}
		item := ""
		for _, it := range c.Items {
			for _, code := range it.Codes {
				if code == in.Code {
					item = it.ID
				}
			}
		}
		if item == "" {
			fail(w, 404, errors.New("未知条码，请在电脑维护物料映射"))
			return
		}
		raw, _ := json.Marshal([]any{d.ID, d.Zone, item, quantity, in.Version})
		digest := sha256.Sum256(raw)
		event := d.ID + ":" + in.Event
		if old, ok := c.Seen[event]; ok {
			if old != digest {
				fail(w, 409, errors.New("同一事件不能修改内容"))
				return
			}
			jsonOut(w, 200, map[string]any{"duplicate": true, "state": captureSnapshot(c, d)})
			return
		}
		if len(c.Seen) >= 2000 {
			fail(w, 409, errors.New("采集单已达2000次提交，请封存并分批"))
			return
		}
		record := captureRecord{ID: event, Item: item, Zone: d.Zone, Device: d.ID, Quantity: quantity, At: now.UTC().Format(time.RFC3339Nano)}
		if c.Mode == "count" {
			at := -1
			for i, old := range c.Records {
				if old.Item == item && old.Zone == d.Zone {
					at = i
					record.Version = old.Version
					break
				}
			}
			if in.Version != record.Version {
				fail(w, 409, errors.New("该盘点行已更新，请刷新后复核"))
				return
			}
			record.Version++
			if at >= 0 {
				c.Records[at] = record
			} else {
				c.Records = append(c.Records, record)
			}
		} else {
			record.Version = 1
			if len(c.Records) >= 500 {
				fail(w, 409, errors.New("最多500条入库记录，请分批"))
				return
			}
			c.Records = append(c.Records, record)
		}
		c.Seen[event] = digest
	default:
		fail(w, 400, errors.New("不支持的设备操作"))
		return
	}
	jsonOut(w, 200, captureSnapshot(c, d))
}
