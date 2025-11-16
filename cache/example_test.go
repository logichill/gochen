package cache_test

import (
	"fmt"
	"time"

	"gochen/cache"
)

// ExampleNew 演示创建缓存
func ExampleNew() {
	// 创建一个简单的字符串缓存
	c := cache.New[string, string](cache.Config{
		Name:    "example",
		MaxSize: 100,
		TTL:     5 * time.Minute,
	})

	c.Set("key", "value")
	value, found := c.Get("key")
	fmt.Println(found, value)
	// Output: true value
}

// ExampleCache_Set 演示设置缓存值
func ExampleCache_Set() {
	c := cache.New[string, int](cache.Config{
		Name:    "numbers",
		MaxSize: 10,
		TTL:     time.Minute,
	})

	c.Set("answer", 42)
	value, _ := c.Get("answer")
	fmt.Println(value)
	// Output: 42
}

// ExampleCache_Get 演示获取缓存值
func ExampleCache_Get() {
	c := cache.New[string, string](cache.Config{
		Name:    "users",
		MaxSize: 100,
		TTL:     time.Hour,
	})

	c.Set("user1", "Alice")

	// 获取存在的键
	value, found := c.Get("user1")
	fmt.Println("存在:", found, value)

	// 获取不存在的键
	_, found = c.Get("user2")
	fmt.Println("不存在:", found)

	// Output:
	// 存在: true Alice
	// 不存在: false
}

// ExampleCache_Delete 演示删除缓存值
func ExampleCache_Delete() {
	c := cache.New[string, string](cache.Config{
		Name:    "temp",
		MaxSize: 10,
		TTL:     time.Minute,
	})

	c.Set("temp_key", "temp_value")
	fmt.Println("删除前:", c.Size())

	c.Delete("temp_key")
	fmt.Println("删除后:", c.Size())

	// Output:
	// 删除前: 1
	// 删除后: 0
}

// ExampleCache_Clear 演示清空缓存
func ExampleCache_Clear() {
	c := cache.New[string, int](cache.Config{
		Name:    "scores",
		MaxSize: 100,
		TTL:     time.Hour,
	})

	c.Set("player1", 100)
	c.Set("player2", 200)
	c.Set("player3", 150)
	fmt.Println("清空前:", c.Size())

	c.Clear()
	fmt.Println("清空后:", c.Size())

	// Output:
	// 清空前: 3
	// 清空后: 0
}

// ExampleCache_Size 演示获取缓存大小
func ExampleCache_Size() {
	c := cache.New[int, string](cache.Config{
		Name:    "items",
		MaxSize: 10,
		TTL:     time.Minute,
	})

	c.Set(1, "one")
	c.Set(2, "two")
	c.Set(3, "three")

	fmt.Println("缓存大小:", c.Size())
	// Output: 缓存大小: 3
}

// Example_userCache 演示用户缓存的完整使用场景
func Example_userCache() {
	// 定义用户类型
	type User struct {
		ID   int64
		Name string
	}

	// 创建用户缓存
	userCache := cache.New[int64, *User](cache.Config{
		Name:    "user_cache",
		MaxSize: 1000,            // 最多缓存1000个用户
		TTL:     5 * time.Minute, // 5分钟过期
	})

	// 缓存用户
	user := &User{ID: 1, Name: "Alice"}
	userCache.Set(user.ID, user)

	// 查询用户
	if cachedUser, found := userCache.Get(1); found {
		fmt.Printf("找到用户: ID=%d, Name=%s\n", cachedUser.ID, cachedUser.Name)
	}

	// 更新用户
	user.Name = "Alice Smith"
	userCache.Set(user.ID, user)

	// 删除用户
	userCache.Delete(1)
	_, found := userCache.Get(1)
	fmt.Println("删除后还存在:", found)

	// Output:
	// 找到用户: ID=1, Name=Alice
	// 删除后还存在: false
}

// Example_aggregateCache 演示聚合对象缓存
func Example_aggregateCache() {
	// 模拟聚合对象
	type OrderAggregate struct {
		ID     int64
		Status string
		Items  []string
	}

	// 创建聚合缓存
	cache := cache.New[int64, *OrderAggregate](cache.Config{
		Name:    "order_aggregate",
		MaxSize: 500,
		TTL:     10 * time.Minute,
	})

	// 缓存聚合
	order := &OrderAggregate{
		ID:     100,
		Status: "pending",
		Items:  []string{"item1", "item2"},
	}
	cache.Set(order.ID, order)

	// 从缓存获取
	if cached, found := cache.Get(100); found {
		fmt.Printf("订单: ID=%d, Status=%s, Items=%d个\n",
			cached.ID, cached.Status, len(cached.Items))
	}

	// Output:
	// 订单: ID=100, Status=pending, Items=2个
}

// Example_sessionCache 演示会话缓存
func Example_sessionCache() {
	// 会话数据
	type Session struct {
		UserID    int64
		Token     string
		ExpiresAt time.Time
	}

	// 创建会话缓存
	sessionCache := cache.New[string, *Session](cache.Config{
		Name:    "session",
		MaxSize: 10000,
		TTL:     30 * time.Minute, // 30分钟会话超时
	})

	// 创建会话
	token := "abc123"
	session := &Session{
		UserID:    1,
		Token:     token,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	sessionCache.Set(token, session)

	// 验证会话
	if sess, found := sessionCache.Get(token); found {
		fmt.Printf("会话有效: UserID=%d\n", sess.UserID)
	} else {
		fmt.Println("会话无效或已过期")
	}

	// Output:
	// 会话有效: UserID=1
}

// Example_lruEviction 演示LRU淘汰机制
func Example_lruEviction() {
	// 创建小容量缓存演示LRU
	c := cache.New[int, string](cache.Config{
		Name:    "lru_demo",
		MaxSize: 3, // 只能存3个
		TTL:     time.Hour,
	})

	// 添加3个元素
	c.Set(1, "one")
	c.Set(2, "two")
	c.Set(3, "three")
	fmt.Println("初始大小:", c.Size())

	// 添加第4个元素，会淘汰最久未使用的（1）
	c.Set(4, "four")
	fmt.Println("添加第4个后:", c.Size())

	// 验证1被淘汰
	_, found := c.Get(1)
	fmt.Println("键1还存在:", found)

	// Output:
	// 初始大小: 3
	// 添加第4个后: 3
	// 键1还存在: false
}

// Example_configCache 演示配置缓存
func Example_configCache() {
	// 配置数据
	type AppConfig struct {
		MaxConnections int
		Timeout        time.Duration
		EnableDebug    bool
	}

	// 创建配置缓存（长TTL）
	configCache := cache.New[string, *AppConfig](cache.Config{
		Name:    "app_config",
		MaxSize: 10,
		TTL:     24 * time.Hour, // 配置1天更新一次
	})

	// 缓存配置
	config := &AppConfig{
		MaxConnections: 100,
		Timeout:        30 * time.Second,
		EnableDebug:    false,
	}
	configCache.Set("app", config)

	// 读取配置
	if cfg, found := configCache.Get("app"); found {
		fmt.Printf("配置: MaxConnections=%d, Timeout=%v\n",
			cfg.MaxConnections, cfg.Timeout)
	}

	// Output:
	// 配置: MaxConnections=100, Timeout=30s
}

// Example_multiTypeCache 演示多类型缓存
func Example_multiTypeCache() {
	// 字符串缓存
	strCache := cache.New[string, string](cache.Config{
		Name:    "strings",
		MaxSize: 100,
		TTL:     time.Minute,
	})
	strCache.Set("name", "Alice")

	// 整数缓存
	intCache := cache.New[string, int](cache.Config{
		Name:    "integers",
		MaxSize: 100,
		TTL:     time.Minute,
	})
	intCache.Set("age", 30)

	// 使用
	name, _ := strCache.Get("name")
	age, _ := intCache.Get("age")
	fmt.Printf("Name: %s, Age: %d\n", name, age)

	// Output:
	// Name: Alice, Age: 30
}
