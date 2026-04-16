// Package dbtool 提供独立的数据库调试工具模式，
// 通过加载 .pb (FileDescriptorSet) 文件发现所有 database_table 消息，
// 连接 Redis 并以可读方式打印数据。
//
// protobuf.go 不依赖任何具体的 proto 扩展定义（如 database_table_options），
// 所有表索引的提取逻辑由业务方通过 TableExtractor 接口注入。
package dbtool

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// IndexType 数据库索引类型
type IndexType int32

const (
	IndexTypeKV        IndexType = 0 // Key-Value
	IndexTypeKL        IndexType = 1 // Key-List
	IndexTypeSortedSet IndexType = 2 // Sorted Set
)

func (t IndexType) String() string {
	switch t {
	case IndexTypeKV:
		return "KV"
	case IndexTypeKL:
		return "KL"
	case IndexTypeSortedSet:
		return "SORTED_SET"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// TableIndex 描述一个数据库表索引
type TableIndex struct {
	Name          string
	Type          IndexType
	EnableCAS     bool
	MaxListLength uint32
	KeyFields     []string
}

// TableInfo 描述一个包含数据库表配置的 proto message
type TableInfo struct {
	MessageFullName protoreflect.FullName
	MessageDesc     protoreflect.MessageDescriptor
	Indexes         []TableIndex
}

// TableExtractor 从 protobuf MessageDescriptor 中提取数据库表索引配置的抽象接口。
// 业务方实现此接口以支持不同的 proto 扩展约定。
type TableExtractor interface {
	// ExtractTableIndexes 检查 message 是否包含数据库表配置，
	// 如果有则返回索引列表，否则返回 nil。
	ExtractTableIndexes(md protoreflect.MessageDescriptor) []TableIndex
}

// Registry 管理所有发现的数据库表消息
type Registry struct {
	tables    map[string]*TableInfo // key: message full name
	resolver  *protoregistry.Types
	files     *protoregistry.Files
	extractor TableExtractor
}

// NewRegistry 创建注册表。extractor 不能为 nil，由业务方提供具体实现。
func NewRegistry(extractor TableExtractor) *Registry {
	return &Registry{
		tables:    make(map[string]*TableInfo),
		resolver:  new(protoregistry.Types),
		files:     new(protoregistry.Files),
		extractor: extractor,
	}
}

// LoadPBFile 加载 .pb (FileDescriptorSet) 文件并发现所有 database_table 消息
func (r *Registry) LoadPBFile(pbFilePath string) error {
	data, err := os.ReadFile(pbFilePath)
	if err != nil {
		return fmt.Errorf("read pb file %s: %w", pbFilePath, err)
	}

	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, fds); err != nil {
		return fmt.Errorf("unmarshal FileDescriptorSet: %w", err)
	}

	// 注册所有 file descriptors
	for _, fdProto := range fds.GetFile() {
		fd, err := protodesc.NewFile(fdProto, r.files)
		if err != nil {
			// 有些依赖文件可能已经全局注册，尝试从全局 registry 中查找
			fd, err = protodesc.NewFile(fdProto, &combinedResolver{local: r.files})
			if err != nil {
				return fmt.Errorf("register file %s: %w", fdProto.GetName(), err)
			}
		}
		if err := r.files.RegisterFile(fd); err != nil {
			// 忽略已注册的文件
			if !strings.Contains(err.Error(), "already registered") {
				return fmt.Errorf("register file %s: %w", fdProto.GetName(), err)
			}
		}
	}

	// 遍历所有文件，查找带 database_table 选项的 message
	r.files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		r.scanMessages(fd.Messages())
		return true
	})

	return nil
}

// scanMessages 递归扫描 message 列表，查找 database_table 选项
func (r *Registry) scanMessages(messages protoreflect.MessageDescriptors) {
	for i := 0; i < messages.Len(); i++ {
		md := messages.Get(i)

		// 通过 extractor 提取表索引配置
		indexes := r.extractor.ExtractTableIndexes(md)
		if len(indexes) > 0 {
			fullName := string(md.FullName())
			r.tables[fullName] = &TableInfo{
				MessageFullName: md.FullName(),
				MessageDesc:     md,
				Indexes:         indexes,
			}
			// 注册为动态消息类型
			mt := dynamicpb.NewMessageType(md)
			r.resolver.RegisterMessage(mt)
		}

		// 递归嵌套消息
		r.scanMessages(md.Messages())
	}
}

// combinedResolver 从本地 registry 和全局 registry 中查找文件
type combinedResolver struct {
	local *protoregistry.Files
}

func (c *combinedResolver) FindFileByPath(path string) (protoreflect.FileDescriptor, error) {
	fd, err := c.local.FindFileByPath(path)
	if err == nil {
		return fd, nil
	}
	return protoregistry.GlobalFiles.FindFileByPath(path)
}

func (c *combinedResolver) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	d, err := c.local.FindDescriptorByName(name)
	if err == nil {
		return d, nil
	}
	return protoregistry.GlobalFiles.FindDescriptorByName(name)
}

// GetAllTables 返回所有已发现的表信息列表（按 message 名排序）
func (r *Registry) GetAllTables() []*TableInfo {
	tables := make([]*TableInfo, 0, len(r.tables))
	for _, t := range r.tables {
		tables = append(tables, t)
	}
	sort.Slice(tables, func(i, j int) bool {
		return tables[i].MessageFullName < tables[j].MessageFullName
	})
	return tables
}

// GetTable 按 message full name 获取表信息
func (r *Registry) GetTable(fullName string) *TableInfo {
	return r.tables[fullName]
}

// GetTableNames 返回所有表的 message 名（短名，不含包名）
func (r *Registry) GetTableNames() []string {
	names := make([]string, 0, len(r.tables))
	for _, t := range r.tables {
		names = append(names, string(t.MessageDesc.Name()))
	}
	sort.Strings(names)
	return names
}

// FindTableByShortName 按 message 短名查找（不含包名）
func (r *Registry) FindTableByShortName(shortName string) *TableInfo {
	for _, t := range r.tables {
		if string(t.MessageDesc.Name()) == shortName {
			return t
		}
	}
	return nil
}

// NewDynamicMessage 创建指定表的动态 proto message 实例
func (r *Registry) NewDynamicMessage(info *TableInfo) *dynamicpb.Message {
	return dynamicpb.NewMessage(info.MessageDesc)
}
