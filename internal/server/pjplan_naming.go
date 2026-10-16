package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type pjplanClass struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	NameZh string `json:"nameZh"`
}

func (a *App) pjplanConfig() (map[string]any, []pjplanClass, error) {
	a.mu.Lock()
	var settings map[string]any
	enabled := false
	for _, plugin := range a.pluginRegistry.Plugins {
		if plugin.ID == "pjplan" {
			enabled = plugin.Enabled
			settings = plugin.Settings
			break
		}
	}
	classesPath := filepath.Join(a.pluginsPath, "pjplan", "classes.json")
	a.mu.Unlock()
	if !enabled {
		return nil, nil, errors.New("请先启用 PjPlan 插件并配置项目版本参数")
	}
	b, err := os.ReadFile(classesPath)
	if err != nil {
		return nil, nil, fmt.Errorf("无法读取 PjPlan 分类目录: %w", err)
	}
	var classes []pjplanClass
	if err := json.Unmarshal(b, &classes); err != nil {
		return nil, nil, fmt.Errorf("PjPlan 分类目录无效: %w", err)
	}
	return settings, classes, nil
}

func pjplanVersion(settings map[string]any) (string, string, string, string, error) {
	project, _ := settings["projectIdentifier"].(string)
	track, _ := settings["versionTrack"].(string)
	major, _ := settings["majorVersion"].(string)
	minor, _ := settings["minorVersion"].(string)
	project, track, major, minor = strings.TrimSpace(project), strings.ToUpper(strings.TrimSpace(track)), strings.ToUpper(strings.TrimSpace(major)), strings.TrimSpace(minor)
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`).MatchString(project) || (track != "P" && track != "R") || !strings.Contains("ABC", major) || len(major) != 1 || !regexp.MustCompile(`^\d{1,3}$`).MatchString(minor) {
		return "", "", "", "", errors.New("请先在 PjPlan 插件设置中填写有效的项目标识、P/R 阶段、A/B/C 大版本和数字小版本")
	}
	return project, track, major, minor, nil
}

func spacedClassCode(code string) string {
	if len(code) == 5 {
		return code[:3] + " " + code[3:]
	}
	return code
}

func (a *App) pjplanFilename(code, ext string) (string, error) {
	settings, classes, err := a.pjplanConfig()
	if err != nil {
		return "", err
	}
	return buildPjplanFilename(settings, classes, code, ext)
}

func buildPjplanFilename(settings map[string]any, classes []pjplanClass, code, ext string) (string, error) {
	project, track, major, minor, err := pjplanVersion(settings)
	if err != nil {
		return "", err
	}
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if !regexp.MustCompile(`^[a-z0-9]{1,12}$`).MatchString(ext) {
		return "", errors.New("扩展名只能包含英文字母和数字")
	}
	for _, item := range classes {
		if strings.ReplaceAll(item.Code, " ", "") == strings.ReplaceAll(strings.TrimSpace(code), " ", "") {
			return spacedClassCode(item.Code) + " " + item.Name + " - " + project + " - " + track + major + minor + "." + ext, nil
		}
	}
	return "", errors.New("分类代码不在已登记的文档目录中")
}

func (a *App) pjplanNextAvailableFilename(dir, code, ext string) (string, error) {
	settings, classes, err := a.pjplanConfig()
	if err != nil {
		return "", err
	}
	start, _ := settings["minorVersion"].(string)
	minor, err := strconv.Atoi(strings.TrimSpace(start))
	if err != nil {
		return "", errors.New("PjPlan 小版本设置无效")
	}
	for i := minor; i <= 999; i++ {
		copySettings := make(map[string]any, len(settings))
		for k, v := range settings {
			copySettings[k] = v
		}
		copySettings["minorVersion"] = strconv.Itoa(i)
		name, err := buildPjplanFilename(copySettings, classes, code, ext)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("当前文档类别的小版本号已用尽，请调整 PjPlan 设置")
}

func (a *App) validatePjplanFilename(filename string) error {
	settings, classes, err := a.pjplanConfig()
	if err != nil {
		return err
	}
	project, track, major, minor, err := pjplanVersion(settings)
	if err != nil {
		return err
	}
	ext := path.Ext(filename)
	if ext == "" {
		return errors.New("文档文件名需要包含扩展名")
	}
	for _, item := range classes {
		expected := spacedClassCode(item.Code) + " " + item.Name + " - " + project + " - " + track + major + minor + ext
		if filename == expected {
			return nil
		}
	}
	return errors.New("新建文档文件名必须由 PjPlan 分类目录和当前项目版本设置生成；请先调用 pjplan_class_lookup，再调用 pjplan_build_filename")
}
