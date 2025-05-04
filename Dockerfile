FROM scratch

COPY ./.out/controller /controller

CMD [ "/controller" ]