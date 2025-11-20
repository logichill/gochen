#!/bin/bash

# Gochen Shared - 将 interface{} 迁移到 any
# Go 1.18+ 推荐使用 any 替代 interface{}

set -e

echo "🔧 开始迁移 interface{} 到 any..."
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 统计修复数量
FIXED_COUNT=0
FILES_MODIFIED=0

# 查找所有需要修复的文件
echo "📂 扫描需要修复的文件..."
FILES=$(find . -name "*.go" -type f ! -path "*/vendor/*" ! -path "*/.git/*")

for file in $FILES; do
    # 检查文件是否包含 interface{}
    if grep -q 'interface{}' "$file"; then
        echo -e "${YELLOW}📝 修复文件: $file${NC}"
        
        # 备份原文件
        cp "$file" "$file.bak"
        
        # 执行替换
        sed -i 's/interface{}/any/g' "$file"
        
        # 统计本文件的修复数量
        COUNT=$(grep -c 'any' "$file" || true)
        FIXED_COUNT=$((FIXED_COUNT + COUNT))
        FILES_MODIFIED=$((FILES_MODIFIED + 1))
        
        echo -e "${GREEN}  ✅ 已迁移${NC}"
    fi
done

echo ""
echo "======================================"
echo -e "${GREEN}✅ 迁移完成！${NC}"
echo "📊 统计信息:"
echo "  - 修改文件数: $FILES_MODIFIED"
echo ""
echo "📝 备份文件已保存为 *.go.bak"
echo "如需恢复，请运行: find . -name '*.go.bak' -exec bash -c 'mv \"\$0\" \"\${0%.bak}\"' {} \;"
echo ""
echo "🔍 建议后续步骤:"
echo "  1. 运行测试: go test ./..."
echo "  2. 运行 gofmt: gofmt -w ."
echo "  3. 查看差异: git diff"
echo "  4. 清理备份: find . -name '*.go.bak' -delete"
echo "======================================"
