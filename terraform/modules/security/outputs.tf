output "step_functions_role_arn" {
  description = "ARN of the role for Step Functions"
  value       = aws_iam_role.step_functions.arn
}

output "ecs_execution_role_arn" {
  description = "ARN of the execution role for ECS"
  value       = aws_iam_role.ecs_execution.arn
}

output "ecs_task_role_arn" {
  description = "ARN of the task role for ECS"
  value       = aws_iam_role.ecs_task.arn
}