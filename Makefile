.PHONY: status master-deploy server-deploy worker-deploy help

Deploy_Path=./deploy

status:
	@echo "################ Status Of Services #################";
	@cd $(Deploy_Path) && docker-compose ps | grep -v go-builder;

master-deploy:
	@echo "################ Deploying Master Project Begin #################" && \
	cd $(Deploy_Path) && docker-compose up go-builder && \
	docker stop gorse-master; docker rm gorse-master; docker-compose up -d gorse-master;
	@echo "################ Deploying Master Project Done #################"

server-deploy:
	@echo "################ Deploying Server Project Begin #################" && \
	cd $(Deploy_Path) && docker-compose up go-builder && \
	docker stop gorse-server; docker rm gorse-server; docker-compose up -d gorse-server;
	@echo "################ Deploying Server Project Done #################"

worker-deploy:
	@echo "################ Deploying Worker Project Begin #################" && \
	cd $(Deploy_Path) && docker-compose up go-builder && \
	docker stop gorse-worker; docker rm gorse-worker; docker-compose up -d gorse-worker;
	@echo "################ Deploying Worker Project Done #################"

help:
	@echo "make status - 查看全部项目服务状态 "
	@echo "make master-deploy - 更新 master"
	@echo "make server-deploy - 更新 server"
	@echo "make worker-deploy - 更新 worker"