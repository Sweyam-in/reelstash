# reelstash-app: the ReelStash media library (image reelstash-ui).
FROM docker.io/library/node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM docker.io/library/nginx:alpine-slim
COPY nginx/api.conf /etc/nginx/reelstash/api.conf
COPY nginx/media.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 3091
CMD ["nginx", "-g", "daemon off;"]
