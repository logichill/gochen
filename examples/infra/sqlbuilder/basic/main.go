package main

import (
	"context"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"

	coredb "gochen/storage/database"
	basicdb "gochen/storage/database/basic"
	sqlbuilder "gochen/storage/database/sql"
)

// demoUser 用于演示 SQL Builder 的简单实体
type demoUser struct {
	ID   int64
	Name string
	Age  int
}

func main() {
	ctx := context.Background()

	// 1. 创建基础数据库（使用 SQLite 内存数据库）
	db, err := basicdb.New(coredb.DBConfig{
		Driver:   "sqlite",
		Database: ":memory:",
	})
	if err != nil {
		log.Fatalf("创建数据库失败: %v", err)
	}
	defer db.Close()

	// 2. 初始化表结构
	if err := initSchema(ctx, db); err != nil {
		log.Fatalf("初始化表失败: %v", err)
	}

	// 3. 构造 ISql
	sql := sqlbuilder.New(db)

	// 4. 插入示例数据
	if err := seedUsers(ctx, sql); err != nil {
		log.Fatalf("插入数据失败: %v", err)
	}

	// 5. 查询所有用户数量
	total, err := countUsers(ctx, sql)
	if err != nil {
		log.Fatalf("统计用户数量失败: %v", err)
	}
	fmt.Printf("总用户数: %d\n", total)

	// 6. 查询 18 岁以上用户，按年龄降序
	users, err := listAdults(ctx, sql, 18)
	if err != nil {
		log.Fatalf("查询成人用户失败: %v", err)
	}
	fmt.Println("18 岁及以上用户:")
	for _, u := range users {
		fmt.Printf("  - ID=%d, Name=%s, Age=%d\n", u.ID, u.Name, u.Age)
	}

	// 7. 演示使用 SetExpr 更新（年龄 +1）
	if err := incrementAge(ctx, sql, users[0].ID); err != nil {
		log.Fatalf("更新用户年龄失败: %v", err)
	}
	fmt.Println("已将第一个用户年龄 +1")

	// 8. 重新查询并打印
	updated, err := listAdults(ctx, sql, 18)
	if err != nil {
		log.Fatalf("重新查询成人用户失败: %v", err)
	}
	fmt.Println("更新后成人用户:")
	for _, u := range updated {
		fmt.Printf("  - ID=%d, Name=%s, Age=%d\n", u.ID, u.Name, u.Age)
	}
}

func initSchema(ctx context.Context, db coredb.IDatabase) error {
	ddl := `
CREATE TABLE IF NOT EXISTS demo_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	age INTEGER NOT NULL,
	created_at DATETIME NOT NULL
);`
	_, err := db.Exec(ctx, ddl)
	return err
}

func seedUsers(ctx context.Context, sql sqlbuilder.ISql) error {
	now := time.Now()
	users := []demoUser{
		{Name: "Alice", Age: 20},
		{Name: "Bob", Age: 17},
		{Name: "Charlie", Age: 30},
	}

	for _, u := range users {
		if _, err := sql.InsertInto("demo_users").
			Columns("name", "age", "created_at").
			Values(u.Name, u.Age, now).
			Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func countUsers(ctx context.Context, sql sqlbuilder.ISql) (int64, error) {
	q, args := sql.Select("COUNT(1)").
		From("demo_users").
		Build()

	db := getDBFromSql(sql)
	row := db.QueryRow(ctx, q, args...)
	var total int64
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func listAdults(ctx context.Context, sql sqlbuilder.ISql, minAge int) ([]demoUser, error) {
	q, args := sql.Select("id", "name", "age").
		From("demo_users").
		Where("age >= ?", minAge).
		OrderBy("age DESC").
		Build()

	db := getDBFromSql(sql)
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []demoUser
	for rows.Next() {
		var u demoUser
		if err := rows.Scan(&u.ID, &u.Name, &u.Age); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func incrementAge(ctx context.Context, sql sqlbuilder.ISql, userID int64) error {
	// 使用 SetExpr 演示复杂表达式更新
	_, err := sql.Update("demo_users").
		SetExpr("age = age + 1").
		Where("id = ?", userID).
		Exec(ctx)
	return err
}

// getDBFromSql 辅助函数：从 sqlImpl 中拿到底层 IDatabase。
//
// 示例中为了简化，使用类型断言；真实项目中建议在构造时显式保存 IDatabase。
func getDBFromSql(sql sqlbuilder.ISql) coredb.IDatabase {
	type hasDB interface {
		GetDB() coredb.IDatabase
	}
	if h, ok := sql.(hasDB); ok {
		return h.GetDB()
	}
	// 退化处理：直接 panic，示例环境足够
	panic("ISql implementation does not expose GetDB()")
}
