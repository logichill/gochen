#!/bin/bash
# 测试运行脚本
# 用法: ./scripts/run-tests.sh [选项]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# 颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

print_section() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════${NC}"
    echo ""
}

# 默认选项
VERBOSE=false
RACE=false
COVERAGE=false
BENCH=false
MODULE=""

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -r|--race)
            RACE=true
            shift
            ;;
        -c|--coverage)
            COVERAGE=true
            shift
            ;;
        -b|--bench)
            BENCH=true
            shift
            ;;
        -m|--module)
            MODULE="$2"
            shift 2
            ;;
        -h|--help)
            echo "用法: $0 [选项]"
            echo ""
            echo "选项:"
            echo "  -v, --verbose    详细输出"
            echo "  -r, --race       竞态检测"
            echo "  -c, --coverage   生成覆盖率报告"
            echo "  -b, --bench      运行性能测试"
            echo "  -m, --module     指定模块 (如: domain/eventsourced)"
            echo "  -h, --help       显示帮助"
            echo ""
            echo "示例:"
            echo "  $0                          # 运行所有测试"
            echo "  $0 -v                       # 详细输出"
            echo "  $0 -c                       # 生成覆盖率"
            echo "  $0 -m domain/eventsourced   # 测试特定模块"
            echo "  $0 -r -c                    # 竞态检测+覆盖率"
            exit 0
            ;;
        *)
            echo "未知选项: $1"
            echo "使用 -h 查看帮助"
            exit 1
            ;;
    esac
done

cd "$PROJECT_DIR"

# 确定测试模块
if [ -n "$MODULE" ]; then
    TEST_MODULES="./$MODULE"
else
    TEST_MODULES="./domain/eventsourced ./bridge ./eventing/projection ./eventing/outbox"
fi

# 构建测试命令
TEST_CMD="go test $TEST_MODULES"

if [ "$VERBOSE" = true ]; then
    TEST_CMD="$TEST_CMD -v"
fi

if [ "$RACE" = true ]; then
    TEST_CMD="$TEST_CMD -race"
fi

if [ "$COVERAGE" = true ]; then
    TEST_CMD="$TEST_CMD -coverprofile=coverage.out -covermode=atomic"
fi

if [ "$BENCH" = true ]; then
    TEST_CMD="$TEST_CMD -bench=. -benchmem"
fi

print_section "运行测试"

echo -e "${YELLOW}命令: $TEST_CMD${NC}"
echo ""

# 运行测试
eval $TEST_CMD

EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✅ 所有测试通过！${NC}"
    
    # 如果生成了覆盖率报告
    if [ "$COVERAGE" = true ] && [ -f "coverage.out" ]; then
        print_section "覆盖率报告"
        
        # 显示覆盖率摘要
        go tool cover -func=coverage.out | tail -20
        
        echo ""
        echo -e "${CYAN}生成 HTML 报告...${NC}"
        go tool cover -html=coverage.out -o coverage.html
        
        echo -e "${GREEN}✅ 覆盖率报告已生成: coverage.html${NC}"
        echo ""
        echo "在浏览器中打开: file://$(pwd)/coverage.html"
    fi
else
    echo ""
    echo -e "${RED}❌ 测试失败！${NC}"
fi

exit $EXIT_CODE
