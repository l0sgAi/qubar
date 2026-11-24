#!/bin/bash

echo "=== 构建诊断脚本 ==="
echo ""

echo "1. 检查 Vite 配置:"
grep -A 2 "base:" vite.config.js
echo ""

echo "2. 检查路由配置:"
grep -A 1 "createWebHistory" src/router/index.js
echo ""

echo "3. 清理并重新构建:"
rm -rf dist
npm run build
echo ""

echo "4. 检查构建产物:"
echo "dist 目录结构:"
ls -la dist/
echo ""

echo "5. 检查 index.html 中的资源路径:"
cat dist/index.html | grep -E "(src=|href=)"
echo ""

echo "6. 检查 assets 目录:"
ls -la dist/assets/ | head -10
echo ""

echo "=== 诊断完成 ==="
