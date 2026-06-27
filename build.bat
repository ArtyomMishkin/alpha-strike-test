@echo off
echo Сборка AlphaStrike.exe ...
go build -ldflags "-s -w" -o AlphaStrike.exe .
if %ERRORLEVEL% NEQ 0 (
    echo Ошибка сборки.
    pause
    exit /b 1
)
echo.
echo Готово: AlphaStrike.exe
echo Запустите двойным щелчком.
pause
