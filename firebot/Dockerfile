FROM python:3.10-slim-bookworm
RUN apt update
RUN apt install -qy python3-pip
RUN apt clean -y
RUN pip install --upgrade pip
RUN pip install wheel
RUN pip install rshell

RUN mkdir /controller
COPY requirements.txt *.py /controller/
WORKDIR /controller
RUN pip install -r requirements.txt

COPY run.sh /controller/

ENTRYPOINT []
CMD /controller/run.sh
