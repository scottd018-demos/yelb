#!/bin/bash
NGINX_CONF=/etc/nginx/conf.d/default.conf
cd clarity-seed

if [ -z "$YELB_APPSERVER_ENDPOINT" ]; then YELB_APPSERVER_ENDPOINT="http://yelb-appserver:4567"; fi

# when the variable is populated a search domain entry is added to resolv.conf at startup
# this is needed for the ECS service discovery given the app works by calling host names and not FQDNs
# a search domain can't be added to the container when using the awsvpc mode
# and the awsvpc mode is needed for A records (bridge only supports SRV records)
if [ $SEARCH_DOMAIN ]; then echo "search ${SEARCH_DOMAIN}" >> /etc/resolv.conf; fi

sed -i -- 's#/usr/share/nginx/html#/clarity-seed/'$UI_ENV'/dist#g' $NGINX_CONF

# this adds the reverse proxy configuration to nginx
# everything that hits /api is proxied to the app server
# NOTE: HACK_PATH is used for knative func as it only handles the path of /.  to handle this
#       we pass an api_path query parameter instead of using the path.
if ! grep -q "location /api" "$NGINX_CONF"; then
    if [ "$HACK_PATH" == "true" ]; then
        RESOLVER="$(cat /etc/resolv.conf | grep nameserver | awk '{print $NF}')"

        eval "cat <<EOF
        location /api/ {
            resolver $RESOLVER;

            if (\\\$request_uri ~ ^([^?]*)) {
                set \\\$api_path \\\$1;
            }

            proxy_pass "$YELB_APPSERVER_ENDPOINT/?api_path=\\\$api_path";
            proxy_http_version 1.1;

            break;
        }
        gzip on;
        gzip_types text/plain text/css application/json application/javascript application/x-javascript text/xml application/xml application/xml+rss text/javascript;
        gunzip on;
EOF
" > /proxycfg.txt
    else
        eval "cat <<EOF
        location /api {
            proxy_pass "$YELB_APPSERVER_ENDPOINT"/api;
            proxy_http_version 1.1;
        }
        gzip on;
        gzip_types text/plain text/css application/json application/javascript application/x-javascript text/xml application/xml application/xml+rss text/javascript;
        gunzip on;
EOF
" > /proxycfg.txt
    fi

    # echo "        proxy_set_header Host $host;" >> /proxycfg.txt
    sed --in-place '/server_name  localhost;/ r /proxycfg.txt' $NGINX_CONF
fi

nginx -g "daemon off;"

