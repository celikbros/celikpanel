# usage: bg.sh SCRIPT ARGS... -- detached run on the host (survives the wsl.exe session)
setsid -f nohup bash "$@" > /dev/null 2>&1 < /dev/null
echo "detached: $*"
