# The bundle is plain JavaScript, so it is built once on the builder's
# platform and copied into the runtime image of each target. Base images are
# pinned to exact tags so that a release says what it was built from;
# Dependabot proposes the updates. Node's major version is the runtime
# image's (nodejs-24-minimal): change both together.
FROM --platform=$BUILDPLATFORM node:24.22.0-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.access.redhat.com/ubi9/nodejs-24-minimal:9.8-1791925262
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
