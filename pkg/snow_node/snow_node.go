package snow_node

import (
	"fmt"
	"github.com/bwmarrin/snowflake"
	"math/rand"
)

var (
	snowNode *snowflake.Node
)

func init() {
	node := int64(rand.Intn(1000))
	snowNode, _ = snowflake.NewNode(node)
}

func GetID() string {
	// Generate a snowflake ID.
	id := snowNode.Generate()
	return fmt.Sprintf("%d", id)
}
