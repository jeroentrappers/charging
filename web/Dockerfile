# Build the PWA once; the API origin is injected at *runtime* (see below).
FROM node:24-alpine AS build
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
# Runtime config: an entrypoint script renders /config.js from this template
# using $VITE_API_BASE before nginx starts (nginx image runs /docker-entrypoint.d/*).
COPY config.template.js /etc/charging/config.template.js
COPY --chmod=0755 docker-entrypoint.d/40-charging-config.sh /docker-entrypoint.d/40-charging-config.sh
EXPOSE 80
