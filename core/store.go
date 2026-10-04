// Package core 实现全部本地核心逻辑：hosts 解析/校验/写入、
// 快照、配置集、JSON 备份、操作日志、手动订阅。
//
// 铁律：
//  1. 任何写入 hosts 的操作之前，必须先通过 ValidateHosts 校验，
//     再自动生成一次安全快照（内容无变化时跳过，避免快照泛滥）。
//  2. 全程零网络请求（subscribe.go 的手动拉取是唯一例外，默认关闭）。
//  3. 所有面向用户的报错都用中文，不抛堆栈。
package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wangzi5151/autohostswitch/platform"
)

// Store 是全部本地数据的入口：hosts 路径 + 数据目录。
type Store struct {
	HostsPath string // 当前生效的 hosts 文件路径
	DataDir   string // 快照/配置/日志存放目录
	Source    string // 操作来源：CLI / Web（记操作日志用）

	// metaMu 保护 meta：Web 每个请求一个 goroutine，
	// ExternalModified（整体覆写）与写入后记哈希（字段写入）并发时会 data race。
	metaMu sync.RWMutex
	meta   meta
}

// getMeta 返回 meta 的副本（读锁）。
func (s *Store) getMeta() meta {
	s.metaMu.RLock()
	defer s.metaMu.RUnlock()
	return s.meta
}

// setMeta 原子替换整个 meta（写锁）。
func (s *Store) setMeta(m meta) {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	s.meta = m
}

// meta 记录初始化状态与“原始备份”快照 ID（紧急恢复用）。
type meta struct {
	Initialized        bool   `json:"initialized"`
	OriginalSnapshotID string `json:"original_snapshot_id"`
	CreatedAt          string `json:"created_at"`
	LastKnownHash      string `json:"last_known_hash"` // 上次成功写入后的 hosts 哈希（外部修改检测用）
}

// NewStore 用系统默认路径创建 Store 并初始化。
func NewStore() (*Store, error) {
	return NewStoreWith(platform.HostsPath(), platform.DataDir())
}

// NewStoreWith 允许指定 hosts 路径与数据目录（测试、绿色版、Termux 非 root 练习用）。
func NewStoreWith(hostsPath, dataDir string) (*Store, error) {
	s := &Store{HostsPath: hostsPath, DataDir: dataDir, Source: "CLI"}
	if err := s.Init(); err != nil {
		return nil, err
	}
	return s, nil
}

// Init 创建数据目录；首次运行时自动把当前 hosts 存为“系统原始备份”。
// 这个备份是紧急恢复的“后悔药”，永远不会被自动删除。
func (s *Store) Init() error {
	for _, d := range []string{s.DataDir, s.snapDir(), s.profileDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("创建数据目录失败（%s）：%w", d, err)
		}
	}
	if err := s.loadMeta(); err != nil {
		return err
	}
	if s.getMeta().Initialized {
		return nil
	}

	// —— 首次运行：备份原始 hosts ——
	now := time.Now().Format("2006-01-02 15:04:05")
	raw, err := os.ReadFile(s.HostsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// hosts 文件尚不存在：记一笔，空内容也算一个起点
			raw = []byte{}
		} else {
			return fmt.Errorf("读取 hosts 文件失败：%w", err)
		}
	}
	snap := Snapshot{
		ID:        newID(),
		Name:      "系统原始备份",
		Note:      "首次运行时自动创建。改错任何东西，用「恢复系统默认快照」都能救回来。",
		CreatedAt: now,
		Auto:      false,
	}
	fillSnapMeta(&snap, raw)
	if err := s.saveSnapshotFile(snap, raw); err != nil {
		return err
	}
	s.setMeta(meta{Initialized: true, OriginalSnapshotID: snap.ID, CreatedAt: now})
	if err := s.saveMeta(); err != nil {
		return err
	}
	s.appendLog("初始化：已自动备份原始 hosts 为快照「系统原始备份」")
	return nil
}

// OriginalSnapshotID 返回首次运行备份的快照 ID（紧急恢复用）。
func (s *Store) OriginalSnapshotID() string { return s.getMeta().OriginalSnapshotID }

// snapDir / profileDir 内部目录。
func (s *Store) snapDir() string    { return filepath.Join(s.DataDir, "snapshots") }
func (s *Store) profileDir() string { return filepath.Join(s.DataDir, "profiles") }
func (s *Store) metaPath() string   { return filepath.Join(s.DataDir, "meta.json") }

func (s *Store) loadMeta() error {
	data, err := os.ReadFile(s.metaPath())
	if err != nil {
		if os.IsNotExist(err) {
			s.setMeta(meta{})
			return nil
		}
		return fmt.Errorf("读取元数据失败：%w", err)
	}
	var m meta
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("元数据损坏（%s），可删除该文件后重试：%w", s.metaPath(), err)
	}
	s.setMeta(m)
	return nil
}

func (s *Store) saveMeta() error {
	data, err := json.MarshalIndent(s.getMeta(), "", "  ")
	if err != nil {
		return err
	}
	// 索引文件也要原子替换：崩溃时不留半截 JSON（见 #4）
	return atomicWriteFile(s.metaPath(), data, 0o644)
}

// atomicWriteFile 原子写入小文件（索引/meta 用）：
// 同目录临时文件 → 写入 → Sync 落盘 → rename 覆盖。
// 崩溃时要么是旧文件、要么是新文件，绝不会留下半截 JSON。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ahs-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	success = true
	return nil
}

// newID 生成快照/配置用的唯一 ID：时间 + 4 字节随机，保证文件名安全。
func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// nowStr 统一的时间格式。
func nowStr() string { return time.Now().Format("2006-01-02 15:04:05") }
