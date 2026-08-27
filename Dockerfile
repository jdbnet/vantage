FROM gcr.io/distroless/static-debian12:nonroot

COPY vantaged /vantaged

USER nonroot
EXPOSE 7687
VOLUME ["/data"]
ENTRYPOINT ["/vantaged"]
CMD ["--data-dir", "/data", "--listen", ":7687"]
