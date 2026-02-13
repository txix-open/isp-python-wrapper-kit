# =====================
# Go builder stage
# =====================
FROM golang:1.25-alpine AS gobuilder

WORKDIR /build

ARG version
ARG app_name

ENV version_env=$version
ENV app_name_env=$app_name

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    go build -ldflags="-X 'main.version=$version_env'" -o /main .


# =====================
# Python deps stage
# =====================
FROM ghcr.io/astral-sh/uv:python3.13-bookworm-slim AS pybuilder

WORKDIR /app

COPY pyproject.toml uv.lock ./
RUN uv sync --frozen

COPY *.py .

# =====================
# Final runtime image
# =====================
FROM alpine:3.22

RUN apk add --no-cache \
    tzdata \
    bash-completion \
    jq \
    python3

# timezone
RUN cp /usr/share/zoneinfo/Europe/Moscow /etc/localtime && \
    echo "Europe/Moscow" > /etc/timezone

ARG app_name
ENV app_name_env=$app_name

# Go binary
COPY --from=gobuilder /main /usr/bin/$app_name_env

# Python app + venv from uv
COPY --from=pybuilder /app /app

# config & autocomplete
COPY /conf/config.yml /etc/$app_name_env/config.yml
COPY /static/autocomplete /etc/bash_completion.d/$app_name_env

WORKDIR /app

CMD ["exec $app_name_env"]