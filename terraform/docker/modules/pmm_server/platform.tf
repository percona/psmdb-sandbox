variable "docker_platform" {
  type        = string
  default     = null
  description = "Optional platform for image pulls and builds; null uses Docker's native platform."
}
