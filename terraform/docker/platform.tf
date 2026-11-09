variable "docker_platform" {
  type        = string
  default     = null
  description = "Image pull/build platform. Null lets Docker use its daemon's native platform; set linux/amd64 or linux/arm64 to override. plgm remains linux/amd64."

  validation {
    condition     = var.docker_platform == null ? true : can(regex("^linux/[a-z0-9_]+(/[a-zA-Z0-9_.-]+)?$", var.docker_platform))
    error_message = "docker_platform must be null or a Linux platform such as linux/amd64, linux/arm64, or linux/arm/v7."
  }
}
