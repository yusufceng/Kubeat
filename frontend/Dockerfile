# The bundle is plain JavaScript, so it is built once on the builder's
# platform and copied into the runtime image of each target. Base images are
# pinned to a digest (the tag is kept for readability) so that a release says
# exactly what it was built from; Dependabot proposes the updates, moving the
# digest and the tag together. Node's major version is the runtime image's
# (nodejs-24-minimal): change both together.
FROM --platform=$BUILDPLATFORM node:24.22.2-slim@sha256:c86ecead9e55a5210ac1727de38a4ca840bf03dcaf34dc9749addbf383b1920c AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.access.redhat.com/ubi9/nodejs-24-minimal:1794242715@sha256:46f0f5e0cb61db71896f74feefe752ac8420eab8eab5e74eb3c35969f65c2550
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
