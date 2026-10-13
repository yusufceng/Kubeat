# The bundle is plain JavaScript, so it is built once on the builder's
# platform and copied into the runtime image of each target.
FROM --platform=$BUILDPLATFORM node:24-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.access.redhat.com/ubi9/nodejs-24-minimal:latest
WORKDIR /app
# The standalone bundle does not include static assets; copy them next to it.
COPY --from=build /src/.next/standalone ./
COPY --from=build /src/.next/static ./.next/static
COPY --from=build /src/public ./public
ENV NODE_ENV=production \
    PORT=3000 \
    HOSTNAME=0.0.0.0
EXPOSE 3000
USER 1001
CMD ["node", "server.js"]
