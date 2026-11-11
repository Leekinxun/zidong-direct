@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

set PURGE_DATA=0
for %%a in (%*) do (
  if /I "%%a"=="--purge-data" set PURGE_DATA=1
)

echo ==^> 正在停止并卸载 紫东直连 (Zidong Direct) 容器与镜像...
where docker >nul 2>nul
if %errorlevel% equ 0 (
  docker compose down --rmi local --volumes --remove-orphans 2>nul
  docker rm -f webdb-studio 2>nul
)

if "%PURGE_DATA%"=="1" (
  if exist "data" (
    echo ==^> 正在删除持久化数据目录 data ...
    rmdir /s /q "data"
  )
) else (
  echo ==^> 已保留持久化数据目录 data (如需一并删除请附加 --purge-data 参数)
)

echo ==^> 紫东直连 (Zidong Direct) 卸载完成。
endlocal
