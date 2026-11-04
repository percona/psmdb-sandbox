variable "environment_id" {
  description = "Exact environment identifier used for container selection."
  type        = string
  default     = ""
}

variable "name" { type = string }
variable "image" { type = string }
variable "network_name" { type = string }
variable "topologies" { type = set(string) }
variable "controller_dir" { type = string }
