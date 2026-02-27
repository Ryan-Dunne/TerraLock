provider "aws" {
  region = "eu-west-1"
}

data "aws_ami" "OtherUbuntu" {
  most_recent = true

  filter {
    name = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }

  owners = ["099720109477"] # Canonical
}

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"

  tags = {
    Name = "main-vpc"
  }
}

resource "aws_internet_gateway" "igw" {
  vpc_id = aws_vpc.main.id
}

resource "aws_instance" "Other_app_server" {
  ami           = data.aws_ami.OtherUbuntu.id
  instance_type = "t3.micro"

  subnet_id              = aws_subnet.OtherPublic.id
  vpc_security_group_ids = [aws_security_group.app_sg.id]

  tags = {
    Name = "Other-app-server"
  }
}

resource "aws_subnet" "OtherPublic" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true

  availability_zone = "eu-west-1a"

  tags = {
    Name = "OtherPublic-subnet"
  }
}

resource "aws_route_table" "OtherPublic" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.igw.id
  }
}

resource "aws_route_table_association" "OtherPublic_assoc" {
  subnet_id      = aws_subnet.OtherPublic.id
  route_table_id = aws_route_table.OtherPublic.id
}

resource "aws_security_group" "Other_app_sg" {
  name        = "app-sg"
  description = "Allow SSH"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "app-sg"
  }
}





