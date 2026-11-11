#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$SCRIPT_DIR"

PURGE_DATA=0
ASSUME_YES=0

for arg in "$@"; do
  case "$arg" in
    --purge-data)
      PURGE_DATA=1
      ;;
    -y|--yes)
      ASSUME_YES=1
      ;;
    -h|--help)
      cat <<'EOF'
紫东直连 (Zidong Direct) 卸载脚本

用法:
  ./uninstall.sh [选项]

选项:
  --purge-data   同时删除 ./data 持久化数据目录（包含加密连接配置与托管 SQLite 文件）
  -y, --yes      跳过交互式确认提示
  -h, --help     显示本帮助信息
EOF
      exit 0
      ;;
    *)
      echo "未知参数: $arg (使用 --help 查看帮助)" >&2
      exit 1
      ;;
  esac
done

echo "==> 准备卸载 紫东直连 (Zidong Direct) Web 工作台..."

if [ "$ASSUME_YES" -ne 1 ]; then
  printf "确认停止并移除 紫东直连 容器与本地镜像吗？[y/N]: "
  read -r answer
  case "$answer" in
    [Yy]|[Yy][Ee][Ss]) ;;
    *)
      echo "已取消卸载。"
      exit 0
      ;;
  esac
fi

if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1 && [ -f "docker-compose.yml" ]; then
    echo "==> 正在通过 docker compose 停止并移除容器与构建镜像..."
    docker compose down --rmi local --volumes --remove-orphans || true
  elif command -v docker-compose >/dev/null 2>&1 && [ -f "docker-compose.yml" ]; then
    echo "==> 正在通过 docker-compose 停止并移除容器与构建镜像..."
    docker-compose down --rmi local --volumes --remove-orphans || true
  fi

  if docker ps -a --format '{{.Names}}' | grep -Eq '^webdb-studio$'; then
    echo "==> 正在移除残留容器 webdb-studio..."
    docker rm -f webdb-studio || true
  fi
fi

if [ "$PURGE_DATA" -eq 1 ]; then
  if [ "$ASSUME_YES" -ne 1 ]; then
    printf "警告：即将永久删除 ./data 目录（含所有已保存连接与托管 SQLite 文件），确认删除吗？[y/N]: "
    read -r purge_ans
    case "$purge_ans" in
      [Yy]|[Yy][Ee][Ss]) ;;
      *)
        echo "==> 已保留 ./data 数据目录。"
        PURGE_DATA=0
        ;;
    esac
  fi
  if [ "$PURGE_DATA" -eq 1 ] && [ -d "$SCRIPT_DIR/data" ]; then
    rm -rf "$SCRIPT_DIR/data"
    echo "==> 已彻底删除持久化数据目录: $SCRIPT_DIR/data"
  fi
else
  echo "==> 已保留持久化数据目录: $SCRIPT_DIR/data (如需连同数据一起删除，请使用 ./uninstall.sh --purge-data)"
fi

echo "==> 紫东直连 (Zidong Direct) 卸载完成。"
