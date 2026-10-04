# PostgreSQL 17 + hypopg (гипотетические индексы для index tuning в Postgres MCP, занятие 2).
# Собирается из официального образа; digest базового образа — infra/images.lock.
FROM postgres:17-alpine
ARG HYPOPG_VERSION=1.4.2
RUN apk add --no-cache --virtual .build-deps git build-base \
    && git clone --depth 1 --branch ${HYPOPG_VERSION} https://github.com/HypoPG/hypopg.git /tmp/hypopg \
    && cd /tmp/hypopg && make with_llvm=no && make install with_llvm=no \
    && rm -rf /tmp/hypopg && apk del .build-deps
