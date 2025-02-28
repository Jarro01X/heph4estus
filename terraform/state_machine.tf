resource "aws_sfn_state_machine" "nmap_scanner" {
  name     = "${var.environment}-nmap-scanner"
  role_arn = aws_iam_role.step_functions.arn

  depends_on = [
    null_resource.wait_for_role_propagation,
    aws_iam_role_policy.step_functions,
    aws_iam_role_policy_attachment.step_functions_custom,
    aws_iam_role_policy_attachment.step_functions_full_access
  ]

  definition = jsonencode({
    Comment = "Nmap scanning workflow"
    StartAt = "ProcessTargets"
    States = {
      ProcessTargets = {
        Type = "Map"
        ItemsPath = "$.targets"
        MaxConcurrency = 10
        Iterator = {
          StartAt = "EnqueueTask"
          States = {
            EnqueueTask = {
              Type = "Task"
              Resource = "arn:aws:states:::sqs:sendMessage"
              Parameters = {
                QueueUrl = aws_sqs_queue.scan_tasks.url
                MessageBody = {
                  "target.$" = "$.target"
                  "options.$" = "$.options"
                }
              }
              Retry = [
                {
                  ErrorEquals = ["States.ALL"]
                  IntervalSeconds = 2
                  MaxAttempts = 3
                  BackoffRate = 2
                }
              ]
              Next = "StartECSTask"
            }
            StartECSTask = {
              Type = "Task"
              Resource = "arn:aws:states:::ecs:runTask.sync"
              Parameters = {
                LaunchType = "FARGATE"
                Cluster = aws_ecs_cluster.scanner.arn
                TaskDefinition = aws_ecs_task_definition.scanner.arn
                NetworkConfiguration = {
                  AwsvpcConfiguration = {
                    Subnets = aws_subnet.private[*].id
                    SecurityGroups = [aws_security_group.ecs_tasks.id]
                  }
                }
              }
              # Add timeout for ECS task - 8 minutes
              TimeoutSeconds = 480
              Retry = [
                {
                  ErrorEquals = ["States.ALL"]
                  IntervalSeconds = 3
                  MaxAttempts = 2
                  BackoffRate = 1.5
                }
              ]
              End = true
            }
          }
        }
        End = true
      }
    }
  })
}