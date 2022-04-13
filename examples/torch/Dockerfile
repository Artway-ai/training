FROM pytorch/pytorch:1.11.0-cuda11.3-cudnn8-runtime

WORKDIR /workspace

RUN apt-get -q update && apt-get -q install -y wget unzip
RUN wget -q http://cs231n.stanford.edu/tiny-imagenet-200.zip && unzip -q tiny-imagenet-200.zip -d data && rm tiny-imagenet-200.zip

# COPY ./tiny-imagenet-200/ /workspace/data/

COPY train.py .
