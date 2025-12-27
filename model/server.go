package model

import (
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"gorm.io/gorm"

	pb "github.com/nezhahq/nezha/proto"
)

type Server struct {
	Common

	Name                   string `json:"name"`
	UUID                   string `json:"uuid,omitempty" gorm:"unique"`
	Note                   string `json:"note,omitempty"`           // 管理员可见备注
	PublicNote             string `json:"public_note,omitempty"`    // 公开备注
	DisplayIndex           int    `json:"display_index"`            // 展示排序，越大越靠前
	HideForGuest           bool   `json:"hide_for_guest,omitempty"` // 对游客隐藏
	EnableDDNS             bool   `json:"enable_ddns,omitempty"`    // 启用DDNS
	DDNSProfilesRaw        string `gorm:"default:'[]';column:ddns_profiles_raw" json:"-"`
	OverrideDDNSDomainsRaw string `gorm:"default:'{}';column:override_ddns_domains_raw" json:"-"`

	DDNSProfiles        []uint64            `gorm:"-" json:"ddns_profiles,omitempty" validate:"optional"` // DDNS配置
	OverrideDDNSDomains map[uint64][]string `gorm:"-" json:"override_ddns_domains,omitempty" validate:"optional"`

	Host       *Host      `gorm:"-" json:"host,omitempty"`
	State      *HostState `gorm:"-" json:"state,omitempty"`
	GeoIP      *GeoIP     `gorm:"-" json:"geoip,omitempty"`
	LastActive time.Time  `gorm:"-" json:"last_active,omitempty"`

	taskStreamMu sync.RWMutex
	TaskStream  pb.NezhaService_RequestTaskServer `gorm:"-" json:"-"`
	ConfigCache chan any                          `gorm:"-" json:"-"`

	PrevTransferInSnapshot  uint64 `gorm:"-" json:"-"` // 上次数据点时的入站使用量
	PrevTransferOutSnapshot uint64 `gorm:"-" json:"-"` // 上次数据点时的出站使用量
}

func InitServer(s *Server) {
	s.Host = &Host{}
	s.State = &HostState{}
	s.GeoIP = &GeoIP{}
	s.ConfigCache = make(chan any, 1)
}

func (s *Server) HasTaskStream() bool {
	s.taskStreamMu.RLock()
	defer s.taskStreamMu.RUnlock()
	return s.TaskStream != nil
}

func (s *Server) SetTaskStream(stream pb.NezhaService_RequestTaskServer) {
	s.taskStreamMu.Lock()
	s.TaskStream = stream
	s.taskStreamMu.Unlock()
}

func (s *Server) ClearTaskStreamIfMatch(stream pb.NezhaService_RequestTaskServer) {
	s.taskStreamMu.Lock()
	if s.TaskStream == stream {
		s.TaskStream = nil
	}
	s.taskStreamMu.Unlock()
}

func (s *Server) SendTask(task *pb.Task) error {
	if task == nil {
		return errors.New("task is nil")
	}

	s.taskStreamMu.Lock()
	defer s.taskStreamMu.Unlock()

	if s.TaskStream == nil {
		return errors.New("task stream not connected")
	}
	if err := s.TaskStream.Send(task); err != nil {
		s.TaskStream = nil
		return err
	}
	return nil
}

func (s *Server) CopyFromRunningServer(old *Server) {
	s.Host = old.Host
	s.State = old.State
	s.GeoIP = old.GeoIP
	s.LastActive = old.LastActive
	old.taskStreamMu.RLock()
	s.TaskStream = old.TaskStream
	old.taskStreamMu.RUnlock()
	s.ConfigCache = old.ConfigCache
	s.PrevTransferInSnapshot = old.PrevTransferInSnapshot
	s.PrevTransferOutSnapshot = old.PrevTransferOutSnapshot
}

func (s *Server) AfterFind(tx *gorm.DB) error {
	if s.DDNSProfilesRaw != "" {
		if err := json.Unmarshal([]byte(s.DDNSProfilesRaw), &s.DDNSProfiles); err != nil {
			log.Println("NEZHA>> Server.AfterFind:", err)
			return nil
		}
	}
	if s.OverrideDDNSDomainsRaw != "" {
		if err := json.Unmarshal([]byte(s.OverrideDDNSDomainsRaw), &s.OverrideDDNSDomains); err != nil {
			log.Println("NEZHA>> Server.AfterFind:", err)
			return nil
		}
	}
	return nil
}

func (s *Server) SplitList(x []*Server) ([]*Server, []*Server) {
	pri := func(s *Server) bool {
		return s.DisplayIndex == 0
	}

	i := slices.IndexFunc(x, pri)
	if i == -1 {
		return nil, x
	}

	return x[:i], x[i:]
}
