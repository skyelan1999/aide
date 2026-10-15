package server

import (
	"fmt"
	"io"
	"os"
)

// Preserve encrypted v1 bytes before a format2 write. A stale migration cannot
// replace a newer file, and an existing backup is never overwritten.
func backupKnowledgeTimeV1(file, expected string) error {
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		if expected != "" {
			return fmt.Errorf("知识历史迁移前原文件已移除；不重建旧历史")
		}
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("知识历史升级源无效；保留原文件")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil || len(raw) > 16<<20 {
		return fmt.Errorf("知识历史升级备份读取失败；保留原文件")
	}
	if expected == "" || hash(raw) != expected {
		return fmt.Errorf("知识历史迁移前版本已变化，请重新读取；保留原文件")
	}
	backup := file + ".before-ai-v2-" + expected[:12] + ".json"
	if existing, err := os.Lstat(backup); err == nil {
		if !existing.Mode().IsRegular() || existing.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("知识历史升级备份目标无效")
		}
		previousFile, err := os.Open(backup)
		if err != nil {
			return err
		}
		previous, err := io.ReadAll(io.LimitReader(previousFile, (16<<20)+1))
		closeErr := previousFile.Close()
		if err != nil || closeErr != nil || len(previous) > 16<<20 || hash(previous) != expected {
			return fmt.Errorf("知识历史升级备份不一致；保留原文件")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	out, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	created, _ := out.Stat()
	_, writeErr := out.Write(raw)
	if writeErr == nil {
		writeErr = out.Sync()
	}
	closeErr = out.Close()
	if writeErr != nil || closeErr != nil {
		if current, e := os.Lstat(backup); e == nil && created != nil && os.SameFile(created, current) {
			_ = os.Remove(backup)
		}
		return fmt.Errorf("知识历史升级备份写入失败；保留原文件")
	}
	return nil
}
