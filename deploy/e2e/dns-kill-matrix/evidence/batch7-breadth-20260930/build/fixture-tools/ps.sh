ps -eo pid,ppid,stat,etime,cmd | grep -vE ' ps -eo|grep ' | grep -iE 'qemu|python|fixture|bootstrap|defunct|Z ' | cut -c1-220
ls -la /root/cp-pair1 | head; ls -la /var/tmp | grep -iE 'pair|b7' 
