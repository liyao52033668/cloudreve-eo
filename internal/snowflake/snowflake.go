package snowflake

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/snowflake"
)

var (
	node *snowflake.Node
	once sync.Once
)

// Init 初始化雪花 ID 生成器
func Init(nodeID int64) error {
	var initErr error
	once.Do(func() {
		var err error
		node, err = snowflake.NewNode(nodeID)
		if err != nil {
			initErr = fmt.Errorf("初始化雪花 ID 生成器失败: %w", err)
		}
	})
	return initErr
}

// Generate 生成一个新的雪花 ID
func Generate() int64 {
	if node == nil {
		// 如果未初始化，使用默认节点 ID 0
		_ = Init(0)
	}
	return node.Generate().Int64()
}
