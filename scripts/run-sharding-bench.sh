#!/bin/bash

set -e

echo "===== 分库分表性能压测 ====="
echo ""

# 检查 Docker 是否运行
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker 未运行，请先启动 Docker"
    exit 1
fi

# 检查 docker-compose 是否安装
if ! command -v docker-compose &> /dev/null; then
    echo "❌ docker-compose 未安装，请先安装 docker-compose"
    exit 1
fi

# 进入项目根目录
cd "$(dirname "$0")/.."

echo ">>> 步骤 1/4: 清理旧环境"
docker-compose down -v > /dev/null 2>&1 || true
echo "✅ 清理完成"

echo ""
echo ">>> 步骤 2/4: 启动数据库实例"
echo "正在启动 9 个 PostgreSQL 实例 (1个单库 + 8个分片库)..."
docker-compose up -d postgres shard_db_0 shard_db_1 shard_db_2 shard_db_3 shard_db_4 shard_db_5 shard_db_6 shard_db_7

echo "等待数据库启动完成..."
sleep 15

# 检查数据库是否启动成功
echo "检查数据库状态..."
if ! docker-compose ps | grep -q "Up"; then
    echo "❌ 数据库启动失败，请查看日志: docker-compose logs"
    exit 1
fi

echo "✅ 数据库启动完成"

echo ""
echo ">>> 步骤 3/4: 编译压测程序"
go build -o bin/shardbench cmd/shardbench/main.go
echo "✅ 编译完成"

echo ""
echo ">>> 步骤 4/4: 运行压测"
echo ""
echo "================================================"
echo ""

./bin/shardbench

echo ""
echo "================================================"
echo ""
echo "✅ 压测完成！"
echo ""
echo "提示："
echo "  - 查看数据库日志: docker-compose logs"
echo "  - 清理环境: make bench-sharding-clean"
echo "  - 详细文档: docs/sharding-benchmark.md"
echo ""
