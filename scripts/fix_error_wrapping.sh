#!/bin/bash

# Gochen Shared - 自动修复错误包装问题
# 将 fmt.Errorf("%v") 替换为 fmt.Errorf("%w")
# 以保证错误链完整性

set -e

echo "🔧 开始修复错误包装问题..."
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 排除目录
EXCLUDE_DIRS="examples vendor .git"

# 统计修复数量
FIXED_COUNT=0
FILES_MODIFIED=0

# 查找所有需要修复的文件
echo "📂 扫描需要修复的文件..."
FILES=$(find . -name "*.go" -type f ! -path "*/examples/*" ! -path "*/vendor/*" ! -path "*/.git/*")

for file in $FILES; do
    # 检查文件是否包含问题模式
    if grep -q 'fmt\.Errorf("[^"]*: %v"' "$file"; then
        echo -e "${YELLOW}📝 修复文件: $file${NC}"
        
        # 备份原文件
        cp "$file" "$file.bak"
        
        # 执行替换
        # 匹配模式: fmt.Errorf("xxx: %v", err) -> fmt.Errorf("xxx: %w", err)
        sed -i 's/fmt\.Errorf("\([^"]*\): %v"/fmt.Errorf("\1: %w"/g' "$file"
        
        # 统计本文件的修复数量
        COUNT=$(grep -c 'fmt\.Errorf("[^"]*: %w"' "$file" || true)
        FIXED_COUNT=$((FIXED_COUNT + COUNT))
        FILES_MODIFIED=$((FILES_MODIFIED + 1))
        
        echo -e "${GREEN}  ✅ 已修复 $COUNT 处${NC}"
    fi
done

echo ""
echo "======================================"
echo -e "${GREEN}✅ 修复完成！${NC}"
echo "📊 统计信息:"
echo "  - 修改文件数: $FILES_MODIFIED"
echo "  - 修复数量: $FIXED_COUNT"
echo ""
echo "📝 备份文件已保存为 *.go.bak"
echo "如需恢复，请运行: find . -name '*.go.bak' -exec bash -c 'mv \"\$0\" \"\${0%.bak}\"' {} \;"
echo ""
echo "🔍 建议后续步骤:"
echo "  1. 运行测试: go test ./..."
echo "  2. 查看差异: git diff"
echo "  3. 清理备份: find . -name '*.go.bak' -delete"
echo "======================================"
