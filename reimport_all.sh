#!/bin/bash

#auth
export TOKEN=`curl -s -X POST http://localhost:9090/auth/login -H "Content-Type: application/json" -d '{"email":"admin@mako.com","password":"admin123"}' | jq -r '.token // empty'`
# eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOjIsImVtYWlsIjoiYWRtaW5AbWFrby5jb20iLCJyb2xlIjoiYWRtaW4iLCJleHAiOjE3ODczNDEwNDMsImlhdCI6MTc4NzI1NDY0M30.sDvFmOzb2H3GxJPKELZeAo1RnHCkzJHoF8fmXEA7JxU
cd /home/ihar/IdeaProjects/makoshop && curl -s -X POST "http://localhost:9090/admin/import-prices?source=multi&workers=8" -H "Authorization: Bearer ${TOKEN}"
