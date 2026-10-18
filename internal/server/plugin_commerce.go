package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Keys are publisher public keys from the reviewed official catalog, never from the uploaded license.
type signedPluginLicense struct {
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type pluginLicenseClaims struct {
	SchemaVersion int    `json:"schemaVersion"`
	LicenseID     string `json:"licenseId"`
	PluginID      string `json:"pluginId"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	DeviceID      string `json:"deviceId"`
	IssuedAt      int64  `json:"issuedAt"`
	ExpiresAt     int64  `json:"expiresAt,omitempty"`
}

func validateMarketDistribution(p marketPlugin) error {
	if p.Distribution == "" || p.Distribution == "public" {
		if p.Commerce != nil || !marketSHA.MatchString(p.SHA256) || p.Bytes <= 0 || p.Bytes > maxPluginBundle {
			return errors.New("公开插件交付信息无效")
		}
		return nil
	}
	if p.Distribution != "private" || p.Package != "" || p.SHA256 != "" || p.Bytes != 0 || p.Commerce == nil {
		return errors.New("私有插件不能包含公开安装包或摘要字段")
	}
	c := p.Commerce
	if (c.Status != "coming_soon" && c.Status != "available") || c.Model != "perpetual-major" || len(c.PriceLabel) > 160 || len(c.PublicKeys) > 8 {
		return errors.New("付费插件商品信息无效")
	}
	if c.PurchaseURL != "" {
		u, err := url.Parse(c.PurchaseURL)
		if err != nil || len(c.PurchaseURL) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return errors.New("购买入口必须是无凭据的 HTTPS 地址")
		}
	}
	for id, encoded := range c.PublicKeys {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if !pluginIDPattern.MatchString(id) || err != nil || len(key) != ed25519.PublicKeySize {
			return errors.New("发布者授权公钥无效")
		}
	}
	if c.Status == "available" && len(c.PublicKeys) == 0 {
		return errors.New("开放销售前必须配置发布者授权公钥")
	}
	return nil
}

// This is an installation identity in AIDE_DATA, independent of workspace, NIC, IP and account display name.
// Moving/reinstalling the data directory requires license reissue; it is not a hardware anti-cloning claim.
func (a *App) pluginLicenseDevice() (string, error) {
	root, err := os.OpenRoot(a.dataPath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	const name = "auth/plugin-license-device"
	raw, err := skillReadCache(root, name, 128)
	if os.IsNotExist(err) {
		raw = make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return "", err
		}
		f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			raw, err = skillReadCache(root, name, 128)
		} else if e != nil {
			return "", e
		} else {
			_, err = f.Write(raw)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", errors.New("设备授权标识损坏，请恢复备份，不自动重置")
	}
	sum := sha256.Sum256(append([]byte("aide-plugin-license-v1:"), raw...))
	return hex.EncodeToString(sum[:]), nil
}
func (a *App) pluginLicenseDeviceAPI(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	locked := a.loadLockStateLocked().Locked
	readonly := sandboxMode(a.settings.SandboxMode) == "read-only"
	a.mu.Unlock()
	if locked || readonly {
		fail(w, 403, errors.New("请解锁工作台并允许本地写入后生成设备授权码"))
		return
	}
	a.marketMu.Lock()
	device, err := a.pluginLicenseDevice()
	a.marketMu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonOut(w, 200, map[string]string{"deviceId": device})
}
func verifyPrivateBundle(p marketPlugin, license signedPluginLicense, device string, raw []byte) error {
	if p.Commerce == nil || p.Commerce.Status != "available" {
		return errors.New("插件尚未开放授权安装")
	}
	if len(raw) == 0 || len(raw) > maxPluginBundle || len(license.Payload) > 8192 {
		return errors.New("请选择有效私有安装包与许可证")
	}
	public, err := base64.StdEncoding.DecodeString(p.Commerce.PublicKeys[license.KeyID])
	if err != nil || len(public) != ed25519.PublicKeySize {
		return errors.New("授权签发者不受信任")
	}
	payload, err := base64.StdEncoding.DecodeString(license.Payload)
	if err != nil {
		return errors.New("许可证编码无效")
	}
	signature, err := base64.StdEncoding.DecodeString(license.Signature)
	if err != nil || !ed25519.Verify(public, append([]byte("aide-plugin-license-v1\n"), payload...), signature) {
		return errors.New("许可证签名无效")
	}
	var c pluginLicenseClaims
	if json.Unmarshal(payload, &c) != nil || c.SchemaVersion != 1 || c.LicenseID == "" || len(c.LicenseID) > 128 || c.DeviceID != device || c.PluginID != p.ID || c.Version != p.Version || c.IssuedAt <= 0 || c.IssuedAt > time.Now().Unix()+300 || (c.ExpiresAt != 0 && (c.ExpiresAt <= time.Now().Unix() || c.ExpiresAt <= c.IssuedAt)) {
		return errors.New("许可证设备、版本或有效期不匹配")
	}
	sum := sha256.Sum256(raw)
	if c.SHA256 != hex.EncodeToString(sum[:]) {
		return errors.New("私有安装包与授权摘要不匹配")
	}
	manifest, _, err := parsePluginBundle(raw)
	if err != nil {
		return err
	}
	if manifest.ID != p.ID || manifest.Version != p.Version {
		return errors.New("私有安装包 ID 或版本不匹配")
	}
	return nil
}

// Multipart limits apply before buffering, including malformed/oversized licenses. No files written to shared cache.
func readPrivateInstall(w http.ResponseWriter, r *http.Request, in any) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPluginBundle+(128<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	var bundle, review []byte
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		name := part.FormName()
		var limit int64
		switch name {
		case "bundle":
			if bundle != nil {
				return nil, errors.New("重复安装包")
			}
			limit = maxPluginBundle
		case "review":
			if review != nil {
				return nil, errors.New("重复确认信息")
			}
			limit = 16384
		default:
			return nil, errors.New("未知上传字段")
		}
		raw, e := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if e != nil {
			return nil, e
		}
		if int64(len(raw)) > limit {
			return nil, errors.New("上传内容过大")
		}
		if name == "bundle" {
			bundle = raw
		} else {
			review = raw
		}
	}
	if len(bundle) == 0 || len(review) == 0 {
		return nil, errors.New("缺少安装包或授权确认信息")
	}
	decoder := json.NewDecoder(strings.NewReader(string(review)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(in); err != nil {
		return nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("确认信息必须是单个 JSON 对象")
	}
	return bundle, nil
}
