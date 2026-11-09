@echo off
chcp 65001 >nul
echo =======================================
echo Сборка Nexus...
echo =======================================

echo [0/3] Останавливаю запущенные процессы Nexus...
taskkill /F /IM nexus.exe >nul 2>&1
if %ERRORLEVEL% equ 0 (
    echo   - Процесс nexus.exe был запущен и успешно остановлен.
)
taskkill /F /IM nexus-dev.exe >nul 2>&1
if %ERRORLEVEL% equ 0 (
    echo   - Процесс nexus-dev.exe был запущен и успешно остановлен.
)
echo   - Проверка завершена.

echo [1/2] Собираю Dev-версию (с консолью)...
go build -o nexus-dev.exe .
if %ERRORLEVEL% neq 0 goto error

echo [2/2] Собираю Prod-версию (без консоли)...
go build -ldflags="-H windowsgui" -o nexus.exe .
if %ERRORLEVEL% neq 0 goto error

echo.
echo =======================================
echo Сборка успешно завершена!
echo =======================================
pause
exit /b 0

:error
echo.
echo ОШИБКА ПРИ СБОРКЕ! Проверь код.
pause
exit /b 1