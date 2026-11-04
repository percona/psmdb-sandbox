variable "environment_id" {
  description = "Exact environment identifier used for container selection."
  type        = string
  default     = ""
}

######
# LDAP
######

variable "ldap_server" {
  default = "ldap"
  type    = string
}

variable "ldap_port" {
  default = 389
  type    = number
}

variable "ldap_image" {
  description = "LDAP Docker image"
  default     = "osixia/openldap:1.5.0"
  type        = string
}

variable "ldap_admin_server" {
  default = "phpldapadmin"
  type    = string
}

variable "ldap_admin_port" {
  default = 80
  type    = number
}

variable "ldap_external_admin_port" {
  default = 8080
  type    = number
}

variable "ldap_admin_image" {
  description = "LDAP Admin Docker image"
  default     = "osixia/phpldapadmin:0.9.0"
  type        = string
}

variable "domain_name" {
  description = "Name of the DNS domain"
  default     = ""
}

variable "env_tag" {
  description = "Name of the Environment"
  default     = "test"
}

variable "ldap_domain" {
  default = "example.com"
}

variable "ldap_org" {
  default = "Example Org"
}

# Admin user is cn=admin,dc=example,dc=org
variable "ldap_admin_password" {
  default = "admin"
  type    = string
}

variable "ldap_users" {
  default = [
    {
      uid      = "alice"
      cn       = "Alice"
      sn       = "Admin"
      password = "secret123"
    }
  ]
}

#############
# Networking
#############

variable "network_name" {
  type    = string
  default = "mongo-terraform"
}

variable "bind_to_localhost" {
  type        = bool
  default     = true
  description = "Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0"
}
