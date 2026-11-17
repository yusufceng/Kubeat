# The bundle is plain JavaScript, so it is built once on the builder's
# platform and copied into the runtime image of each target. Base images are
# pinned to a digest (the tag is kept for readability) so that a release says
# exactly what it was built from; Dependabot proposes the updates, moving the
# digest and the tag together. Node's major version is the runtime image's
# (nodejs-24-minimal): change both together.
FROM --platform=$BUILDPLATFORM node:24.22.3-slim@sha256:dc26ae92c9fb15cf6b1d63e20f0944b2efe0f115b20e00be48fb23fceb061c54 AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.access.redhat.com/ubi9/nodejs-24-minimal:1793614965@sha256:da724df0db4baceb0b89ebdc160dfe330fe3b215925742ee9e65efdfc6038850
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
