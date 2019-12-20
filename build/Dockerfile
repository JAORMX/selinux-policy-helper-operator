# Step one: build compliance-operator
FROM registry.access.redhat.com/ubi8/go-toolset as builder

WORKDIR /go/src/github.com/JAORMX/selinux-policy-helper-operator

ENV GOFLAGS=-mod=vendor

COPY . .
RUN make TARGET_DIR=/tmp

# Step two: containerize compliance-operator
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/selinux-policy-helper-operator \
    USER_UID=1001 \
    USER_NAME=selinux-policy-helper-operator

# install operator binary
COPY --from=builder /tmp/selinux-policy-helper-operator ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
