#!/bin/bash

set -u

role='master'
uid='1'

printf -v tablet_dir 'vt_%010d' $uid

tablet_role='master'

dbconfig_dba_flags="\
    -db-config-dba-uname root \
    -db-config-dba-charset utf8"

dbconfig_flags="\
    -db-config-app-uname root \
    -db-config-app-charset utf8 \
    -db-config-app-host 127.0.0.1 \
    -db-config-app-port 3306"

init_db_sql_file="$VTROOT/init_db.sql"
echo "GRANT ALL ON *.* TO 'root'@'%';" > $init_db_sql_file
echo "GRANT ALL ON *.* TO 'root'@'127.0.0.1';" > $init_db_sql_file

if [ "$tablet_role" = "master" ]; then
    echo "CREATE DATABASE dbx_demo_db;" >> $init_db_sql_file
    echo "create table dbx_demo_db.dbx_demo_user( user_id bigint, name varchar(128), primary key (user_id));" >> $init_db_sql_file
fi

export EXTRA_MY_CNF=$VTROOT/config/mycnf/master_mysql56.cnf

mkdir -p $VTDATAROOT/backups

echo "Starting MySQL for tablet..."
action="init -init_db_sql_file $init_db_sql_file"
if [ -d $VTDATAROOT/$tablet_dir ]; then
  echo "Resuming from existing vttablet dir:"
  echo "    $VTDATAROOT/$tablet_dir"
  action='start'
fi

$VTROOT/bin/mysqlctl \
  -log_dir $VTDATAROOT/tmp \
  -tablet_uid $uid \
  -tablet_dir $tablet_dir \
  $dbconfig_dba_flags \
  -mysql_port 3306 \
  $action

mysql_auth_server_static_file=$VTROOT/src/github.com/youtube/vitess/examples/vtqueryserver/auth.json

echo -e "Start a new client: mysql -h 127.0.0.1 -P 3307 -u demo -pdemo\n"
echo -e "Try an insert: insert into dbx_demo_db.dbx_demo_user  (user_id, name) values (1, 'chum');\n"
echo -e "Try a select: select * from dbx_demo_db.dbx_demo_user;\n"
echo "Explore the query stats http://localhost:3308/queryz or status report http://localhost:3308/debug/status"

$VTROOT/src/github.com/youtube/vitess/go/cmd/vtqueryserver/vtqueryserver $dbconfig_flags -mysqlproxy_server_port 3307 -port 3308 -mysql_auth_server_static_file=$mysql_auth_server_static_file
